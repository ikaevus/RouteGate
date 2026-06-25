package vpnaccounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCredentialsReturnsVLESSRealityProfile(t *testing.T) {
	repo := &fakeAccountRepository{
		profile: SubscriptionProfile{
			Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, ServerID: "server-1", VLESSUUID: testVLESSUUID},
			Server: &SubscriptionServer{
				ID:                "server-1",
				Name:              "Finland",
				Hostname:          "fi.routegate.example",
				VLESSPort:         443,
				VLESSFlow:         "xtls-rprx-vision",
				VLESSNetwork:      "tcp",
				RealityPublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
				RealityShortID:    "0123456789abcdef",
				RealityServerName: "www.example.com",
			},
			Credentials: SubscriptionCredentials{
				VLESS: VLESSCredentials{
					UUID:    testVLESSUUID,
					Flow:    "xtls-rprx-vision",
					Network: "tcp",
				},
				Reality: RealityCredentials{
					PublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
					ShortID:    "0123456789abcdef",
					ServerName: "www.example.com",
				},
			},
		},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/credentials", nil)
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.GetCredentials(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body=%s", http.StatusOK, response.Code, response.Body.String())
	}
	var body VLESSRealityCredentialsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.VPNAccountID != "account-1" || body.ServerID != "server-1" || body.Protocol != "vless" {
		t.Fatalf("unexpected credentials response identity: %+v", body)
	}
	if body.Endpoint != "fi.routegate.example" {
		t.Fatalf("expected hostname endpoint, got %q", body.Endpoint)
	}
	if body.VLESS.UUID != testVLESSUUID || body.VLESS.Flow != "xtls-rprx-vision" || body.VLESS.Network != "tcp" {
		t.Fatalf("unexpected VLESS credentials: %+v", body.VLESS)
	}
	if !body.Reality.Enabled || body.Reality.PublicKey != "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0" || body.Reality.ShortID != "0123456789abcdef" || body.Reality.ServerName != "www.example.com" {
		t.Fatalf("unexpected Reality credentials: %+v", body.Reality)
	}
}
