package db

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/servers"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type protocolExpectation struct {
	primary string
	active  []string
}

// The active protocols of each account must follow the Agent-confirmed apply
// of the version that deploys them: first issue on a non-VLESS node, a saved
// but unapplied switch, a failed apply, a successful apply and a rollback by
// re-applying the older version, for single- and multi-protocol accounts.
func TestActiveProtocolsFollowTheAppliedVersion(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	key := newRealityTestKey(t)
	serverID, singleID, multiID := createAppliedSettingsServer(t, ctx, pool, key)
	if _, err := pool.Exec(ctx, `UPDATE servers SET vpn_protocol = 'shadowsocks' WHERE id = $1::uuid`, serverID); err != nil {
		t.Fatalf("use Shadowsocks as node protocol: %v", err)
	}

	accounts := vpnaccounts.NewRepository(pool)
	serverRepo := servers.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	shadowsocksOnly := protocolExpectation{"shadowsocks", []string{"shadowsocks"}}

	// v1 deploys Shadowsocks for both accounts. Neither has a client profile
	// row yet, so the first issue must start from the applied protocol, not
	// from the 'vless' column default.
	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertActiveProtocols(t, ctx, accounts, subscription, singleID, shadowsocksOnly)

	// Saved but unapplied: the node switches to VLESS and the multi-protocol
	// account asks for VLESS + Shadowsocks with VLESS as primary. The first
	// save creates the multi account's rows; the deployed Shadowsocks row
	// must stay active and VLESS must stay pending.
	vless := "vless"
	if _, err := serverRepo.UpdateProtocolSettings(ctx, serverID, servers.UpdateProtocolSettingsInput{Protocol: &vless}); err != nil {
		t.Fatalf("save node protocol switch: %v", err)
	}
	template, err := accounts.GetOrCreateClientProfile(ctx, singleID)
	if err != nil {
		t.Fatalf("read profile template: %v", err)
	}
	vlessPrimary := vpnaccounts.UpdateClientProfileRequest{
		Name: template.Name, ClientType: template.ClientType, DeviceType: template.DeviceType,
		FingerprintMode: template.FingerprintMode, Fingerprint: template.Fingerprint, SpiderX: template.SpiderX,
		MTU: template.MTU, Protocol: "vless",
	}
	if _, err := accounts.UpdateClientProfileWithProtocols(ctx, multiID, vlessPrimary, []string{"vless", "shadowsocks"}); err != nil {
		t.Fatalf("save multi-protocol preference: %v", err)
	}
	assertActiveProtocols(t, ctx, accounts, subscription, singleID, shadowsocksOnly)
	assertActiveProtocols(t, ctx, accounts, subscription, multiID, shadowsocksOnly)
	assertDesiredProtocols(t, ctx, accounts, multiID, []string{"vless", "shadowsocks"})

	// A rendered version whose apply fails changes nothing.
	v2 := renderVersion(t, ctx, render, serverID)
	if v2 == v1 {
		t.Fatal("protocol changes must render a new config version")
	}
	finishApply(t, ctx, pool, serverID, v2, "failed")
	assertActiveProtocols(t, ctx, accounts, subscription, singleID, shadowsocksOnly)
	assertActiveProtocols(t, ctx, accounts, subscription, multiID, shadowsocksOnly)

	// The successful apply of v2 releases exactly what v2 deploys. The single
	// account never chose protocols, so "auto" follows the node's new default:
	// its seeded Shadowsocks row does not keep Shadowsocks deployed.
	vlessOnly := protocolExpectation{"vless", []string{"vless"}}
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertActiveProtocols(t, ctx, accounts, subscription, singleID, vlessOnly)
	assertActiveProtocols(t, ctx, accounts, subscription, multiID, protocolExpectation{"vless", []string{"vless", "shadowsocks"}})

	// A preference saved after v2 must not become active through a rollback.
	if _, err := accounts.UpdateClientProfileWithProtocols(ctx, multiID, vlessPrimary, []string{"vless"}); err != nil {
		t.Fatalf("save newer preference: %v", err)
	}

	// Rolling back by re-applying v1 restores v1's protocols for every account
	// it deploys, although the saved preferences still ask for VLESS.
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertActiveProtocols(t, ctx, accounts, subscription, singleID, shadowsocksOnly)
	assertActiveProtocols(t, ctx, accounts, subscription, multiID, shadowsocksOnly)
	assertDesiredProtocols(t, ctx, accounts, multiID, []string{"vless"})

	// Re-applying v2 moves both accounts forward again.
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertActiveProtocols(t, ctx, accounts, subscription, singleID, vlessOnly)
	assertActiveProtocols(t, ctx, accounts, subscription, multiID, protocolExpectation{"vless", []string{"vless", "shadowsocks"}})
}

type subscriptionProbe struct {
	handler *vpnaccounts.Handler
	tokens  map[string]string
	ctx     context.Context
	pool    *pgxpool.Pool
	repo    *vpnaccounts.Repository
}

func newSubscriptionProbe(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *vpnaccounts.Repository) *subscriptionProbe {
	t.Helper()
	return &subscriptionProbe{
		handler: vpnaccounts.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), pool, "https://manager.example.com"),
		tokens:  map[string]string{}, ctx: ctx, pool: pool, repo: repo,
	}
}

// body fetches the raw subscription exactly as a client does through /sub/.
func (p *subscriptionProbe) body(t *testing.T, accountID string) string {
	t.Helper()
	token, ok := p.tokens[accountID]
	if !ok {
		token = "applied-protocols-" + accountID
		if _, err := p.repo.CreateSubscriptionToken(p.ctx, vpnaccounts.CreateSubscriptionTokenInput{
			VPNAccountID: accountID, TokenHash: vpnaccounts.HashSubscriptionToken(token),
		}); err != nil {
			t.Fatalf("create subscription token: %v", err)
		}
		p.tokens[accountID] = token
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/"+token+"?format=raw", nil)
	request.SetPathValue("token", token)
	response := httptest.NewRecorder()
	p.handler.GetClientSubscription(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("subscription status = %d; body=%s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

var protocolLinkScheme = map[string]string{"vless": "vless://", "shadowsocks": "ss://"}

func assertActiveProtocols(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, subscription *subscriptionProbe, accountID string, want protocolExpectation) {
	t.Helper()
	connection, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID)
	if err != nil {
		t.Fatalf("build client connection: %v", err)
	}
	if connection.Protocol != want.primary {
		t.Fatalf("primary protocol = %q, want %q", connection.Protocol, want.primary)
	}
	got := make([]string, 0, len(connection.Connections))
	for _, item := range connection.Connections {
		got = append(got, item.Protocol)
		link := item.VLESSLink + item.ShadowsocksURI
		if !strings.HasPrefix(link, protocolLinkScheme[item.Protocol]) || !strings.Contains(link, "@203.0.113.10:") {
			t.Fatalf("%s link %q does not target the node", item.Protocol, link)
		}
	}
	if !reflect.DeepEqual(got, want.active) {
		t.Fatalf("active protocols = %v, want %v", got, want.active)
	}
	var deployed []string
	var deployedPrimary string
	if err := subscription.pool.QueryRow(ctx, `
		SELECT ARRAY(SELECT jsonb_array_elements_text(cv.client_settings -> 'accounts' -> a.id::text -> 'protocols')),
		       COALESCE(cv.client_settings -> 'accounts' -> a.id::text ->> 'primary', '')
		FROM vpn_accounts a
		JOIN servers s ON s.id = a.server_id
		JOIN config_versions cv ON cv.id = s.active_config_version_id
		WHERE a.id = $1::uuid
	`, accountID).Scan(&deployed, &deployedPrimary); err != nil {
		t.Fatalf("read active version deployment: %v", err)
	}
	if !reflect.DeepEqual(got, deployed) || connection.Protocol != deployedPrimary {
		t.Fatalf("client material (%s %v) differs from the active version (%s %v)", connection.Protocol, got, deployedPrimary, deployed)
	}

	// Links are matched per line: "ss://" also occurs inside "vless://".
	body := subscription.body(t, accountID)
	if !bodyHasLink(body, protocolLinkScheme[want.primary]) {
		t.Fatalf("subscription does not serve the applied primary %s: %q", want.primary, body)
	}
	for protocol, scheme := range protocolLinkScheme {
		if !containsString(want.active, protocol) && bodyHasLink(body, scheme) {
			t.Fatalf("subscription serves %s which the applied version does not deploy: %q", protocol, body)
		}
	}
}

func assertDesiredProtocols(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, accountID string, want []string) {
	t.Helper()
	desired, _, err := accounts.GetClientProtocolSets(ctx, accountID)
	if err != nil {
		t.Fatalf("read protocol sets: %v", err)
	}
	if !reflect.DeepEqual(desired, want) {
		t.Fatalf("desired protocols = %v, want %v", desired, want)
	}
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
