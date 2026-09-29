package db

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// On an MTProto-only node the proxy uses one node-wide secret and the render
// lists no per-account MTProto access, so a new profile has no active_enabled
// row. GET /client-profile, GET /client-connection and /sub/ must still
// evaluate the same connection: served while the applied version runs the
// proxy, withheld once it does not. Versions belong to the test database only.
func TestMTProtoOnlyNodeProfileMatchesServedConnection(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	if _, err := pool.Exec(ctx, `UPDATE servers SET vpn_protocol = 'mtproto', mtproto_port = 9443 WHERE id = $1::uuid`, serverID); err != nil {
		t.Fatalf("switch node to MTProto only: %v", err)
	}

	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler

	secret := nodeMTProtoSecret(t, ctx, pool, serverID)
	v1 := renderVersion(t, ctx, render, serverID)
	assertVersionMTProto(t, ctx, pool, v1, secret, true)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")

	state := readClientProfileState(t, handler, accountID)
	if state.ConnectionStatus != vpnaccounts.ClientConnectionStatusReady || state.ActiveProtocol != "mtproto" ||
		!containsString(state.Profile.ActiveProtocols, "mtproto") {
		t.Fatalf("MTProto-only node after apply: status=%s primary=%s active=%v message=%q",
			state.ConnectionStatus, state.ActiveProtocol, state.Profile.ActiveProtocols, state.ConnectionMessage)
	}
	status, body := callAccountHandler(handler.GetClientConnection, http.MethodGet, accountID, "")
	if status != http.StatusOK || !strings.Contains(body, "tg://proxy") {
		t.Fatalf("client connection on MTProto-only node: status=%d", status)
	}
	if status, body := subscription.fetch(t, accountID); status != http.StatusOK || !strings.Contains(body, "tg://proxy") {
		t.Fatalf("subscription on MTProto-only node: status=%d body_has_link=%v", status, strings.Contains(body, "://"))
	}

	// The next version must really drop the MTProto proxy and its shared
	// secret: seeded protocol rows must not keep it in the render.
	if _, err := pool.Exec(ctx, `UPDATE servers SET vpn_protocol = 'vless' WHERE id = $1::uuid`, serverID); err != nil {
		t.Fatalf("switch node away from MTProto: %v", err)
	}
	v2 := renderVersion(t, ctx, render, serverID)
	assertVersionMTProto(t, ctx, pool, v2, secret, false)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")

	state = readClientProfileState(t, handler, accountID)
	if containsString(state.Profile.ActiveProtocols, "mtproto") {
		t.Fatalf("profile still reports MTProto without the applied proxy: status=%s active=%v", state.ConnectionStatus, state.Profile.ActiveProtocols)
	}
	status, body = callAccountHandler(handler.GetClientConnection, http.MethodGet, accountID, "")
	if strings.Contains(body, "tg://") {
		t.Fatalf("client connection serves MTProto without the applied proxy: status=%d", status)
	}
	if state.ConnectionStatus == vpnaccounts.ClientConnectionStatusReady && status != http.StatusOK ||
		state.ConnectionStatus != vpnaccounts.ClientConnectionStatusReady && status == http.StatusOK {
		t.Fatalf("profile status %s disagrees with client connection status %d", state.ConnectionStatus, status)
	}
	if status, body := subscription.fetch(t, accountID); strings.Contains(body, "tg://") {
		t.Fatalf("subscription serves MTProto without the applied proxy: status=%d", status)
	}
	if state.ConnectionStatus != vpnaccounts.ClientConnectionStatusReady || !containsString(state.Profile.ActiveProtocols, "vless") {
		t.Fatalf("after the proxy was removed the account must be served its VLESS access: status=%s active=%v message=%q",
			state.ConnectionStatus, state.Profile.ActiveProtocols, state.ConnectionMessage)
	}
}
