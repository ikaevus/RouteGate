package db

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/servers"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// Version numbers in these tests belong to the isolated test database only.

// "auto" stands for the protocol the next render deploys: the node's saved
// default. A profile edit made after saving a node protocol switch, but before
// applying it, must be validated against that saved default, while clients
// keep the previously applied connection until an apply succeeds.
func TestAutoPreferenceFollowsTheSavedNodeProtocol(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler

	saveNodeProtocol(t, ctx, pool, serverID, "shadowsocks")
	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"shadowsocks"}, []string{"shadowsocks"})

	// Save the switch to VLESS without applying it.
	saveNodeProtocol(t, ctx, pool, serverID, "vless")
	stale := `{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"auto","enabledProtocols":["shadowsocks"]}`
	if status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, accountID, stale); status != http.StatusBadRequest ||
		!strings.Contains(body, "node-default") {
		t.Fatalf("auto without the saved node default must be refused: status=%d body=%s", status, body)
	}
	status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, accountID,
		`{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"auto","enabledProtocols":["vless"]}`)
	if status != http.StatusOK || strings.Contains(body, "vless://") {
		t.Fatalf("auto with the saved node default: status=%d vless_link=%v", status, strings.Contains(body, "vless://"))
	}

	// The working Shadowsocks connection stays until the switch is applied.
	state := readClientProfileState(t, handler, accountID)
	assertProfileState(t, state, vpnaccounts.ClientConnectionStatusReady, []string{"vless"}, []string{"shadowsocks"})
	assertServedLinks(t, handler, subscription, accountID, "ss://", "vless://")

	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"vless"}, []string{"vless"})
	assertServedLinks(t, handler, subscription, accountID, "vless://", "ss://")
}

// A protocol row Manager seeds automatically must not keep the node-wide
// MTProto proxy running after the node is switched to another protocol. The
// switch takes effect only through a successful apply, and re-applying the
// MTProto version (a rollback) restores MTProto access.
func TestSeededMTProtoDoesNotKeepTheProxyAfterANodeSwitch(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler

	saveNodeProtocol(t, ctx, pool, serverID, "mtproto")
	secret := nodeMTProtoSecret(t, ctx, pool, serverID)
	// Reading the profile seeds protocol rows before anything is applied.
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusAwaitingFirstApply, []string{"mtproto"}, []string{})
	assertPublicSubscriptionWithheld(t, subscription, accountID)

	v1 := renderVersion(t, ctx, render, serverID)
	assertVersionMTProto(t, ctx, pool, v1, secret, true)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"mtproto"}, []string{"mtproto"})
	assertServedLinks(t, handler, subscription, accountID, "tg://", "vless://")

	// Saved but unapplied: MTProto keeps working, the switch is pending.
	saveNodeProtocol(t, ctx, pool, serverID, "vless")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"vless"}, []string{"mtproto"})
	assertServedLinks(t, handler, subscription, accountID, "tg://", "vless://")

	v2 := renderVersion(t, ctx, render, serverID)
	assertVersionMTProto(t, ctx, pool, v2, secret, false)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"vless"}, []string{"vless"})
	assertServedLinks(t, handler, subscription, accountID, "vless://", "tg://")

	// Rollback to the MTProto version restores MTProto access.
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"vless"}, []string{"mtproto"})
	assertServedLinks(t, handler, subscription, accountID, "tg://", "vless://")

	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertServedLinks(t, handler, subscription, accountID, "vless://", "tg://")

	// An account created after this apply gets no public subscription either.
	newID := createActiveAccount(t, ctx, pool, serverID)
	assertPublicSubscriptionWithheld(t, subscription, newID)
}

// An administrator's explicit MTProto choice survives a node switch: the
// proxy stays in the render and the account keeps MTProto access.
func TestExplicitMTProtoChoiceSurvivesANodeSwitch(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler

	saveNodeProtocol(t, ctx, pool, serverID, "mtproto")
	secret := nodeMTProtoSecret(t, ctx, pool, serverID)
	if status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, accountID,
		`{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"mtproto","enabledProtocols":["mtproto"]}`); status != http.StatusOK {
		t.Fatalf("explicit MTProto choice: status=%d body=%s", status, body)
	}
	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertServedLinks(t, handler, subscription, accountID, "tg://", "vless://")

	saveNodeProtocol(t, ctx, pool, serverID, "vless")
	v2 := renderVersion(t, ctx, render, serverID)
	assertVersionMTProto(t, ctx, pool, v2, secret, true)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertProfileState(t, readClientProfileState(t, handler, accountID), vpnaccounts.ClientConnectionStatusReady, []string{"mtproto"}, []string{"mtproto"})
	assertServedLinks(t, handler, subscription, accountID, "tg://", "vless://")
}

func saveNodeProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID, protocol string) {
	t.Helper()
	if _, err := servers.NewRepository(pool).UpdateProtocolSettings(ctx, serverID, servers.UpdateProtocolSettingsInput{Protocol: &protocol}); err != nil {
		t.Fatalf("save node protocol %s: %v", protocol, err)
	}
}

func nodeMTProtoSecret(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID string) string {
	t.Helper()
	var secret string
	if err := pool.QueryRow(ctx, `SELECT mtproto_secret FROM servers WHERE id = $1::uuid`, serverID).Scan(&secret); err != nil || len(secret) < 32 {
		t.Fatalf("read node MTProto secret: %v", err)
	}
	return secret
}

// assertVersionMTProto checks both the rendered runtime config and the client
// settings snapshot of a version for the node's MTProto proxy and secret.
func assertVersionMTProto(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, secret string, want bool) {
	t.Helper()
	var rendered []byte
	var snapshot struct {
		MTProtoSecret string `json:"mtprotoSecret"`
		MTProtoPort   int    `json:"mtprotoPort"`
	}
	var snapshotJSON []byte
	if err := pool.QueryRow(ctx, `SELECT rendered_config, client_settings FROM config_versions WHERE id = $1::uuid`, versionID).
		Scan(&rendered, &snapshotJSON); err != nil {
		t.Fatalf("read config version: %v", err)
	}
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		t.Fatalf("decode client settings snapshot: %v", err)
	}
	var config struct {
		MTProto string `json:"mtproto"`
	}
	if err := json.Unmarshal(rendered, &config); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	hasProxy := strings.TrimSpace(config.MTProto) != ""
	hasSecret := strings.Contains(string(rendered), secret)
	snapshotHasProxy := snapshot.MTProtoSecret != "" && snapshot.MTProtoPort > 0
	if hasProxy != want || hasSecret != want || snapshotHasProxy != want {
		t.Fatalf("version MTProto proxy=%v secret_in_render=%v snapshot=%v, want %v", hasProxy, hasSecret, snapshotHasProxy, want)
	}
}

// assertServedLinks checks that GET /client-connection and /sub/ agree: the
// wanted scheme is served, the other one is not.
func assertServedLinks(t *testing.T, handler *vpnaccounts.Handler, subscription *subscriptionProbe, accountID, want, absent string) {
	t.Helper()
	// JSON string values start with a quote, so "ss:// never matches inside
	// "vless://.
	status, body := callAccountHandler(handler.GetClientConnection, http.MethodGet, accountID, "")
	hasWant, hasAbsent := strings.Contains(body, `"`+want), strings.Contains(body, `"`+absent)
	if status != http.StatusOK || !hasWant || hasAbsent {
		t.Fatalf("client connection: status=%d has %s=%v has %s=%v", status, want, hasWant, absent, hasAbsent)
	}
	status, body = subscription.fetch(t, accountID)
	if status != http.StatusOK || !bodyHasLink(body, want) || bodyHasLink(body, absent) {
		t.Fatalf("subscription: status=%d has %s=%v has %s=%v", status, want, bodyHasLink(body, want), absent, bodyHasLink(body, absent))
	}
}

// assertPublicSubscriptionWithheld checks the JSON subscription
// (GET /api/v1/subscriptions/{token}) hands out no client material for access
// the node does not serve.
func assertPublicSubscriptionWithheld(t *testing.T, subscription *subscriptionProbe, accountID string) {
	t.Helper()
	subscription.fetch(t, accountID) // issues the token
	token := subscription.tokens[accountID]
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/"+token, nil)
	request.SetPathValue("token", token)
	response := httptest.NewRecorder()
	subscription.handler.GetPublicSubscription(response, request)
	var body struct {
		Config struct {
			Status   string          `json:"status"`
			Rendered json.RawMessage `json:"rendered"`
		} `json:"config"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode public subscription: %v", err)
	}
	if body.Config.Status != "unavailable" || len(body.Config.Rendered) > 0 && string(body.Config.Rendered) != "null" ||
		strings.Contains(response.Body.String(), "://") {
		t.Fatalf("public subscription must withhold access: status=%s", body.Config.Status)
	}
}
