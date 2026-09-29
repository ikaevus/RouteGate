package db

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type clientProfileState struct {
	Profile struct {
		Protocol         string   `json:"protocol"`
		EnabledProtocols []string `json:"enabledProtocols"`
		ActiveProtocols  []string `json:"activeProtocols"`
	} `json:"profile"`
	ActiveProtocol    string `json:"activeProtocol"`
	ConnectionStatus  string `json:"connectionStatus"`
	ConnectionMessage string `json:"connectionMessage"`
}

// Administrators must always be able to read and edit an account's saved
// protocol preferences, while client access stays withheld until the node has
// been given it. Version numbers belong to the isolated test database only.
func TestClientProfileStateStaysEditableWhileAccessAwaitsApply(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, existingID, _ := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))

	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler

	// First run: nothing has ever been applied on the node.
	state := readClientProfileState(t, handler, existingID)
	assertProfileState(t, state, vpnaccounts.ClientConnectionStatusAwaitingFirstApply, []string{"vless"}, []string{})
	assertAccessWithheld(t, handler, subscription, existingID)

	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	state = readClientProfileState(t, handler, existingID)
	assertProfileState(t, state, vpnaccounts.ClientConnectionStatusReady, []string{"vless"}, []string{"vless"})

	// An account created after the apply: preferences are readable and
	// editable, access is withheld, the message names the next action.
	newID := createActiveAccount(t, ctx, pool, serverID)
	state = readClientProfileState(t, handler, newID)
	assertProfileState(t, state, vpnaccounts.ClientConnectionStatusAwaitingApply, []string{"vless"}, []string{})
	if !strings.Contains(state.ConnectionMessage, "render and successfully apply") {
		t.Fatalf("awaiting-apply message must name the next action: %q", state.ConnectionMessage)
	}
	assertAccessWithheld(t, handler, subscription, newID)
	status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, newID,
		`{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"vless","enabledProtocols":["vless","shadowsocks"]}`)
	if status != http.StatusOK || strings.Contains(body, "://") {
		t.Fatalf("saving preferences for an undeployed account: status=%d body=%s", status, body)
	}
	state = readClientProfileState(t, handler, newID)
	assertProfileState(t, state, vpnaccounts.ClientConnectionStatusAwaitingApply, []string{"vless", "shadowsocks"}, []string{})

	// Adding a protocol to an account the node already serves keeps its
	// working access and shows the new protocol as pending.
	saveProtocols(t, ctx, accounts, existingID, existingID, "vless", []string{"vless", "shadowsocks"})
	state = readClientProfileState(t, handler, existingID)
	assertProfileState(t, state, vpnaccounts.ClientConnectionStatusReady, []string{"vless", "shadowsocks"}, []string{"vless"})

	// The successful apply of the next configuration returns both accounts
	// to the normal state and releases client access.
	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	for _, accountID := range []string{existingID, newID} {
		state = readClientProfileState(t, handler, accountID)
		assertProfileState(t, state, vpnaccounts.ClientConnectionStatusReady, []string{"vless", "shadowsocks"}, []string{"vless", "shadowsocks"})
		if status, _ := callAccountHandler(handler.GetClientConnection, http.MethodGet, accountID, ""); status != http.StatusOK {
			t.Fatalf("client connection after apply: status %d", status)
		}
		if status, body := subscription.fetch(t, accountID); status != http.StatusOK || !strings.Contains(body, "vless://") {
			t.Fatalf("subscription after apply: status=%d body=%q", status, body)
		}
	}
}

func readClientProfileState(t *testing.T, handler *vpnaccounts.Handler, accountID string) clientProfileState {
	t.Helper()
	status, body := callAccountHandler(handler.GetClientProfile, http.MethodGet, accountID, "")
	if status != http.StatusOK {
		t.Fatalf("client profile state: status=%d body=%s", status, body)
	}
	if strings.Contains(body, "://") {
		t.Fatalf("client profile state must never carry client links: %s", body)
	}
	var state clientProfileState
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatalf("decode client profile state: %v", err)
	}
	return state
}

func assertProfileState(t *testing.T, state clientProfileState, status string, desired, active []string) {
	t.Helper()
	if state.ConnectionStatus != status || !reflect.DeepEqual(state.Profile.EnabledProtocols, desired) ||
		!reflect.DeepEqual(nonNil(state.Profile.ActiveProtocols), active) {
		t.Fatalf("state=%s desired=%v active=%v, want %s %v %v (message %q)",
			state.ConnectionStatus, state.Profile.EnabledProtocols, state.Profile.ActiveProtocols, status, desired, active, state.ConnectionMessage)
	}
}

func assertAccessWithheld(t *testing.T, handler *vpnaccounts.Handler, subscription *subscriptionProbe, accountID string) {
	t.Helper()
	if status, body := callAccountHandler(handler.GetClientConnection, http.MethodGet, accountID, ""); status != http.StatusConflict ||
		!strings.Contains(body, "client_connection_unavailable") || strings.Contains(body, "://") {
		t.Fatalf("client connection must stay withheld: status=%d body=%s", status, body)
	}
	if status, body := subscription.fetch(t, accountID); status == http.StatusOK || strings.Contains(body, "://") {
		t.Fatalf("subscription must stay withheld: status=%d body=%q", status, body)
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
