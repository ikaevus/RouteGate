package vpnaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func preImportSource() clientConnectionTestSource {
	accountID := "11111111-1111-4111-8111-111111111111"
	applied := SubscriptionServer{
		ID: "node-1", PublicIP: "203.0.113.10", VLESSPort: 443,
		RealityPublicKey: "old-applied-key", RealityServerName: "old.example.com",
		AwaitingFirstApply: false,
	}
	applied.useAppliedClientSettings(platform.AppliedClientSettings{
		Accounts: map[string]platform.AppliedAccountProtocols{},
		VLESSPort: 443, VLESSNetwork: "tcp", RealityPublicKey: "old-applied-key",
		RealityServerName: "old.example.com",
	}, accountID)
	saved := SubscriptionServer{
		ID: "node-1", PublicIP: "203.0.113.10", VLESSPort: 8443,
		VLESSFlow: "xtls-rprx-vision", VLESSNetwork: "ws",
		RealityPublicKey: "future-key", RealityServerName: "www.microsoft.com",
		RealityShortID: "abcd1234", VPNProtocol: ClientProtocolVLESS,
	}
	subscription := SubscriptionProfile{
		Account: Account{
			ID: accountID, DisplayName: "Pending Account", Status: StatusActive,
			ServerID: "node-1", VLESSUUID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		},
		Server: &applied, savedServer: &saved,
	}
	return clientConnectionTestSource{
		subscription: subscription,
		profile: ClientProfile{Protocol: ClientProtocolAuto, FingerprintMode: FingerprintModeAuto, SpiderX: "/"},
	}
}

func TestUnappliedVLESSPreviewUsesSavedConfigWithoutServingPublicSubscription(t *testing.T) {
	source := preImportSource()
	result, err := BuildUnappliedVLESSPreview(context.Background(), source, source.subscription.Account.ID)
	if err != nil {
		t.Fatalf("build preliminary preview: %v", err)
	}
	if result.Status != "unapplied_preview" || result.Protocol != "vless" || result.Format != "vless-reality-uri" {
		t.Fatalf("incorrect non-ready label: %+v", result)
	}
	parsed, err := url.Parse(result.VLESSURI)
	if err != nil || parsed.Scheme != "vless" {
		t.Fatalf("expected direct VLESS share URI, not an HTTP subscription: %v", err)
	}
	if parsed.Query().Get("pbk") != "future-key" || parsed.Query().Get("sni") != "www.microsoft.com" ||
		parsed.Query().Get("type") != "tcp" || parsed.Query().Get("flow") != "xtls-rprx-vision" ||
		parsed.Port() != "8443" {
		t.Fatalf("incorrect saved preview parameters: %q", parsed.RawQuery)
	}
	if !strings.Contains(result.Warning, "PRELIMINARY") {
		t.Fatalf("warning must clearly identify preliminary access: %s", result.Warning)
	}
	// The original applied settings must remain unchanged.
	if source.subscription.Server.RealityPublicKey != "old-applied-key" {
		t.Fatal("preview mutated the applied server")
	}
	_, err = BuildClientConnection(context.Background(), source, source.subscription.Account.ID)
	if !errors.Is(err, ErrAccountProtocolNotDeployed) {
		t.Fatalf("normal applied-only delivery was accidentally relaxed: %v", err)
	}
}

func TestUnappliedVLESSPreviewFailClosed(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	tests := []struct {
		name   string
		change func(*clientConnectionTestSource)
		want   error
	}{
		{"ready access", func(s *clientConnectionTestSource) {
			s.subscription.Server.deployment.protocols = []string{ClientProtocolVLESS}
		}, ErrPreImportNotPending},
		{"unknown deployment", func(s *clientConnectionTestSource) {
			s.subscription.Server.deployment = nil
		}, ErrPreImportNotPending},
		{"legacy snapshot unknown", func(s *clientConnectionTestSource) {
			s.subscription.Server.deployment.accountsKnown = false
		}, ErrPreImportNotPending},
		{"suspended account", func(s *clientConnectionTestSource) {
			s.subscription.Account.Status = StatusSuspended
		}, ErrPreImportNotAllowed},
		{"created account", func(s *clientConnectionTestSource) {
			s.subscription.Account.Status = StatusCreated
		}, ErrPreImportNotAllowed},
		{"expired account", func(s *clientConnectionTestSource) {
			s.subscription.Account.ExpiresAt = &past
		}, ErrPreImportNotAllowed},
		{"unassigned account", func(s *clientConnectionTestSource) {
			s.subscription.Server = nil
		}, ErrPreImportNotAllowed},
		{"desired protocol is not VLESS", func(s *clientConnectionTestSource) {
			s.profile.Protocol = ClientProtocolHysteria2
		}, ErrPreImportNotAllowed},
		{"no saved settings", func(s *clientConnectionTestSource) {
			s.subscription.savedServer = nil
		}, ErrPreImportNotPrepared},
		{"no Reality public key", func(s *clientConnectionTestSource) {
			s.subscription.savedServer.RealityPublicKey = ""
		}, ErrPreImportNotPrepared},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := preImportSource()
			tc.change(&source)
			result, err := BuildUnappliedVLESSPreview(context.Background(), source, source.subscription.Account.ID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("wanted %v, got %v", tc.want, err)
			}
			if result.VLESSURI != "" {
				t.Fatal("refused preview must not expose credentials")
			}
		})
	}
}

func TestUnappliedVLESSPreviewFirstApply(t *testing.T) {
	source := preImportSource()
	source.subscription.Server.AwaitingFirstApply = true
	source.subscription.Server.deployment = nil
	result, err := BuildUnappliedVLESSPreview(context.Background(), source, source.subscription.Account.ID)
	if err != nil || !strings.HasPrefix(result.VLESSURI, "vless://") {
		t.Fatalf("expected explicitly preliminary access before first apply: %v", err)
	}
}

func TestPreImportEndpointRequiresAcknowledgementAndDisablesCaching(t *testing.T) {
	source := preImportSource()
	repo := &fakeAccountRepository{profile: source.subscription}
	handler := newTestHandler(repo)
	tests := []struct {
		name string
		body string
		want int
	}{
		{"not acknowledged", `{"acknowledgeUnapplied":false}`, http.StatusBadRequest},
		{"missing acknowledgement", `{}`, http.StatusBadRequest},
		{"unknown fields", `{"acknowledgeUnapplied":true,"unsafe":true}`, http.StatusBadRequest},
		{"second JSON object", `{"acknowledgeUnapplied":true}{}`, http.StatusBadRequest},
		{"explicitly acknowledged", `{"acknowledgeUnapplied":true}`, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/client-connection/pre-import", strings.NewReader(tc.body))
			request.SetPathValue("id", source.subscription.Account.ID)
			response := httptest.NewRecorder()
			handler.PreviewUnappliedVLESS(response, request)
			if response.Code != tc.want {
				t.Fatalf("status %d, want %d; body %s", response.Code, tc.want, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("secret response headers missing: %#v", response.Header())
			}
			if tc.want == http.StatusOK {
				var preview UnappliedVLESSPreview
				if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
					t.Fatal(err)
				}
				if preview.Status != "unapplied_preview" || !strings.HasPrefix(preview.VLESSURI, "vless://") {
					t.Fatalf("wrong response: %+v", preview)
				}
			} else if strings.Contains(response.Body.String(), "vless://") {
				t.Fatal("rejected request exposed sensitive configuration")
			}
		})
	}
}
