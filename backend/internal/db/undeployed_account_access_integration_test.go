package db

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// Version numbers in these tests belong to the isolated test database only.

// Accounts created or activated after the last successful apply are absent
// from the node configuration. No delivery path may hand them access until a
// new configuration that includes them is rendered and applied.
func TestAccountsMissingFromTheAppliedConfigGetNoAccess(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, existingID, reactivatedID := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	setAccountStatus(t, ctx, pool, reactivatedID, "suspended")

	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)

	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertServedProtocols(t, ctx, accounts, subscription, existingID, "vless", []string{"vless"})

	// Created after the apply, and reactivated after the render: the node has
	// neither account's credentials.
	newID := createActiveAccount(t, ctx, pool, serverID)
	setAccountStatus(t, ctx, pool, reactivatedID, "active")
	for _, accountID := range []string{newID, reactivatedID} {
		assertAwaitingDeployment(t, ctx, accounts, subscription, accountID)
	}
	// The admin UI link endpoint reports the same state with the next action,
	// and saving a profile for the account still works (without any link).
	uiStatus, uiBody := callAccountHandler(subscription.handler.GetClientConnection, http.MethodGet, newID, "")
	if uiStatus != http.StatusConflict || !strings.Contains(uiBody, "client_connection_unavailable") ||
		!strings.Contains(uiBody, "render and successfully apply") || strings.Contains(uiBody, "vless://") {
		t.Fatalf("UI client connection status=%d body=%s", uiStatus, uiBody)
	}
	saveStatus, saveBody := callAccountHandler(subscription.handler.UpdateClientProfile, http.MethodPatch, newID,
		`{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"auto","enabledProtocols":["vless"]}`)
	if saveStatus != http.StatusOK || strings.Contains(saveBody, "vless://") {
		t.Fatalf("saving a profile for an undeployed account: status=%d body=%s", saveStatus, saveBody)
	}
	// Nothing changed for the account the node already serves.
	assertServedProtocols(t, ctx, accounts, subscription, existingID, "vless", []string{"vless"})

	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	for _, accountID := range []string{existingID, newID, reactivatedID} {
		assertServedProtocols(t, ctx, accounts, subscription, accountID, "vless", []string{"vless"})
	}
}

// A multi-protocol set is served whole or not at all: a protocol the applied
// configuration does not deploy for the account never appears next to working
// links, even when its active flag says otherwise.
func TestMultiProtocolSetNeverIncludesAnUndeployedProtocol(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, templateID, multiID := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))

	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)

	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertServedProtocols(t, ctx, accounts, subscription, multiID, "vless", []string{"vless"})

	// Adding Shadowsocks is a pending preference: VLESS keeps working alone.
	saveProtocols(t, ctx, accounts, templateID, multiID, "vless", []string{"vless", "shadowsocks"})
	assertServedProtocols(t, ctx, accounts, subscription, multiID, "vless", []string{"vless"})

	// Even a stale active flag cannot put an undeployed link into the set.
	if _, err := pool.Exec(ctx, `
		UPDATE vpn_account_protocols SET active_enabled = TRUE
		WHERE vpn_account_id = $1::uuid AND protocol = 'shadowsocks'
	`, multiID); err != nil {
		t.Fatalf("simulate stale active flag: %v", err)
	}
	assertAwaitingDeployment(t, ctx, accounts, subscription, multiID)

	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertServedProtocols(t, ctx, accounts, subscription, multiID, "vless", []string{"vless", "shadowsocks"})
}

// MTProto uses one node-wide secret and is not listed per account. Its access
// follows the applied MTProto proxy: an apply must not switch it off for an
// account that also uses a listed protocol, and it ends once the applied
// version no longer runs the proxy.
func TestMTProtoAccessFollowsTheAppliedProxy(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, templateID, mixedID := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))

	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)

	saveProtocols(t, ctx, accounts, templateID, mixedID, "vless", []string{"vless", "mtproto"})
	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertServedProtocols(t, ctx, accounts, subscription, mixedID, "vless", []string{"vless", "mtproto"})

	// v2 no longer runs the MTProto proxy.
	saveProtocols(t, ctx, accounts, templateID, mixedID, "vless", []string{"vless"})
	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertServedProtocols(t, ctx, accounts, subscription, mixedID, "vless", []string{"vless"})

	// A stale MTProto flag cannot resurrect access while the proxy is not
	// part of the applied configuration.
	if _, err := pool.Exec(ctx, `
		UPDATE vpn_account_protocols SET active_enabled = TRUE
		WHERE vpn_account_id = $1::uuid AND protocol = 'mtproto'
	`, mixedID); err != nil {
		t.Fatalf("simulate stale MTProto flag: %v", err)
	}
	assertAwaitingDeployment(t, ctx, accounts, subscription, mixedID)
}

func setAccountStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE vpn_accounts SET status = $2 WHERE id = $1::uuid`, accountID, status); err != nil {
		t.Fatalf("set account status %s: %v", status, err)
	}
}

func createActiveAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID string) string {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id, vless_uuid)
		VALUES ('acct-' || substr(md5(random()::text), 1, 8), 'sing-box', 'Created later', 'active', $1::uuid, gen_random_uuid())
		RETURNING id::text
	`, serverID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID
}

func saveProtocols(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, templateID, accountID, primary string, enabled []string) {
	t.Helper()
	template, err := accounts.GetOrCreateClientProfile(ctx, templateID)
	if err != nil {
		t.Fatalf("read profile template: %v", err)
	}
	request := vpnaccounts.UpdateClientProfileRequest{
		Name: template.Name, ClientType: template.ClientType, DeviceType: template.DeviceType,
		FingerprintMode: template.FingerprintMode, Fingerprint: template.Fingerprint, SpiderX: template.SpiderX,
		MTU: template.MTU, Protocol: primary,
	}
	if _, err := accounts.UpdateClientProfileWithProtocols(ctx, accountID, request, enabled); err != nil {
		t.Fatalf("save protocols %v: %v", enabled, err)
	}
}

func callAccountHandler(handler http.HandlerFunc, method, accountID, body string) (int, string) {
	request := httptest.NewRequest(method, "/api/v1/vpn-accounts/"+accountID+"/client-profile", strings.NewReader(body))
	request.SetPathValue("id", accountID)
	response := httptest.NewRecorder()
	handler(response, request)
	return response.Code, response.Body.String()
}

// fetch returns the raw /sub/ response without failing on non-200 statuses.
func (p *subscriptionProbe) fetch(t *testing.T, accountID string) (int, string) {
	t.Helper()
	token, ok := p.tokens[accountID]
	if !ok {
		token = "undeployed-access-" + accountID
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
	return response.Code, response.Body.String()
}

func assertAwaitingDeployment(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, subscription *subscriptionProbe, accountID string) {
	t.Helper()
	_, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID)
	if !errors.Is(err, vpnaccounts.ErrClientConnectionUnavailable) || !errors.Is(err, vpnaccounts.ErrAccountProtocolNotDeployed) ||
		!strings.Contains(err.Error(), "render and successfully apply") {
		t.Fatalf("expected client_connection_unavailable with the render-and-apply action, got %v", err)
	}
	status, body := subscription.fetch(t, accountID)
	if status == http.StatusOK {
		t.Fatalf("subscription served undeployed access: %q", body)
	}
	if strings.Contains(body, "://") {
		t.Fatalf("subscription response leaks client material: %q", body)
	}
}

func assertServedProtocols(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, subscription *subscriptionProbe, accountID, primary string, want []string) {
	t.Helper()
	connection, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID)
	if err != nil {
		t.Fatalf("build client connection: %v", err)
	}
	got := []string{}
	for _, item := range connection.Connections {
		got = append(got, item.Protocol)
		if item.VLESSLink+item.ShadowsocksURI+item.MTProtoURI == "" {
			t.Fatalf("%s connection has no client material", item.Protocol)
		}
	}
	if connection.Protocol != primary || !reflect.DeepEqual(got, want) {
		t.Fatalf("served %s %v, want %s %v", connection.Protocol, got, primary, want)
	}
	// The raw subscription carries the primary protocol's material.
	status, body := subscription.fetch(t, accountID)
	if status != http.StatusOK || !strings.Contains(body, "vless://") {
		t.Fatalf("subscription status=%d body=%q", status, body)
	}
	if !containsString(want, "shadowsocks") && bodyHasLink(body, "ss://") {
		t.Fatalf("subscription serves undeployed Shadowsocks: %q", body)
	}
}

// bodyHasLink reports a subscription line starting with scheme ("ss://" is a
// substring of "vless://", so a plain substring check would be wrong).
func bodyHasLink(body, scheme string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), scheme) {
			return true
		}
	}
	return false
}
