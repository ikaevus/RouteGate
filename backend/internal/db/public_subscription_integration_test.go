package db

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// Version numbers in these tests belong to the isolated test database only.

// A saved but unapplied primary protocol must not change the JSON
// subscription: like GET /client-connection and /sub/, it keeps serving the
// connection the node applied until an apply deploys the new preference.
func TestPublicSubscriptionServesTheAppliedPrimaryUntilApply(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler

	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertPublicSubscription(t, subscription, accountID, "sing-box", "vless")

	if status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, accountID,
		`{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"shadowsocks","enabledProtocols":["shadowsocks"]}`); status != http.StatusOK {
		t.Fatalf("save Shadowsocks as the new primary: status=%d body=%s", status, body)
	}
	assertServedLinks(t, handler, subscription, accountID, "vless://", "ss://")
	assertPublicSubscription(t, subscription, accountID, "sing-box", "vless")

	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertServedLinks(t, handler, subscription, accountID, "ss://", "vless://")
	assertPublicSubscription(t, subscription, accountID, "shadowsocks", "shadowsocks")
}

// While the node runs the MTProto proxy for another account, a saved but
// unapplied MTProto preference must not hand this account the MTProto link
// and the node-wide secret through the JSON subscription.
func TestPublicSubscriptionWithholdsAnUnappliedMTProtoPreference(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	serverID, accountID, otherID := createAppliedSettingsServer(t, ctx, pool, newRealityTestKey(t))
	accounts := vpnaccounts.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	subscription := newSubscriptionProbe(t, ctx, pool, accounts)
	handler := subscription.handler
	secret := nodeMTProtoSecret(t, ctx, pool, serverID)
	mtprotoOnly := `{"clientType":"generic","deviceType":"other","fingerprintMode":"auto","protocol":"mtproto","enabledProtocols":["mtproto"]}`

	if status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, otherID, mtprotoOnly); status != http.StatusOK {
		t.Fatalf("save MTProto for the other account: status=%d body=%s", status, body)
	}
	v1 := renderVersion(t, ctx, render, serverID)
	assertVersionMTProto(t, ctx, pool, v1, secret, true)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertPublicSubscription(t, subscription, otherID, "mtproto", "mtproto")
	assertPublicSubscription(t, subscription, accountID, "sing-box", "vless")

	if status, body := callAccountHandler(handler.UpdateClientProfile, http.MethodPatch, accountID, mtprotoOnly); status != http.StatusOK {
		t.Fatalf("save MTProto for the VLESS account: status=%d body=%s", status, body)
	}
	assertServedLinks(t, handler, subscription, accountID, "vless://", "tg://")
	body := assertPublicSubscription(t, subscription, accountID, "sing-box", "vless")
	if strings.Contains(body, secret) {
		t.Fatal("the JSON subscription leaks the node-wide MTProto secret before the preference is applied")
	}

	v2 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertServedLinks(t, handler, subscription, accountID, "tg://", "vless://")
	assertPublicSubscription(t, subscription, accountID, "mtproto", "mtproto")
}

type publicSubscriptionBody struct {
	Server struct {
		Endpoint string `json:"endpoint"`
	} `json:"server"`
	Config struct {
		Type     string `json:"type"`
		Status   string `json:"status"`
		Rendered *struct {
			Format  string          `json:"format"`
			Content json.RawMessage `json:"content"`
			Text    string          `json:"text"`
		} `json:"rendered"`
	} `json:"config"`
}

// fetchPublicSubscription returns GET /api/v1/subscriptions/{token}.
func fetchPublicSubscription(t *testing.T, subscription *subscriptionProbe, accountID string) (publicSubscriptionBody, string) {
	t.Helper()
	subscription.fetch(t, accountID) // issues the token
	token := subscription.tokens[accountID]
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/"+token, nil)
	request.SetPathValue("token", token)
	response := httptest.NewRecorder()
	subscription.handler.GetPublicSubscription(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("public subscription status %d", response.Code)
	}
	var body publicSubscriptionBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode public subscription: %v", err)
	}
	return body, response.Body.String()
}

// assertPublicSubscription checks that the JSON subscription renders exactly
// the served protocol, agrees with GET /client-connection, and that its server
// endpoint matches the node the connection targets.
func assertPublicSubscription(t *testing.T, subscription *subscriptionProbe, accountID, configType, protocol string) string {
	t.Helper()
	body, raw := fetchPublicSubscription(t, subscription, accountID)
	if body.Config.Status != "rendered" || body.Config.Type != configType || body.Config.Rendered == nil {
		t.Fatalf("public subscription config type=%s status=%s, want rendered %s", body.Config.Type, body.Config.Status, configType)
	}
	material := body.Config.Rendered.Text + string(body.Config.Rendered.Content)
	schemes := map[string]string{"vless": `"type":"vless"`, "shadowsocks": "ss://", "mtproto": "tg://"}
	for candidate, marker := range schemes {
		if has := strings.Contains(material, marker); has != (candidate == protocol) {
			t.Fatalf("public subscription %s material present=%v, want only %s", candidate, has, protocol)
		}
	}
	status, connection := callAccountHandler(subscription.handler.GetClientConnection, http.MethodGet, accountID, "")
	if status != http.StatusOK || !strings.Contains(connection, `"protocol":"`+protocol+`"`) {
		t.Fatalf("client connection disagrees with the public subscription (%s): status=%d", protocol, status)
	}
	if body.Server.Endpoint != "203.0.113.10" {
		t.Fatalf("public subscription server endpoint = %q", body.Server.Endpoint)
	}
	return raw
}
