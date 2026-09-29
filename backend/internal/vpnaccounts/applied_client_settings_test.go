package vpnaccounts

import (
	"errors"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func TestClientConnectionUnavailableBeforeFirstApply(t *testing.T) {
	subscription := SubscriptionProfile{
		Account:     Account{ID: "account-id", VLESSUUID: "11111111-1111-4111-8111-111111111111"},
		Server:      &SubscriptionServer{PublicIP: "203.0.113.10", VLESSPort: 8443, RealityPublicKey: "pbk", RealityShortID: "sid", RealityServerName: "www.microsoft.com", AwaitingFirstApply: true},
		Credentials: SubscriptionCredentials{VLESS: VLESSCredentials{UUID: "11111111-1111-4111-8111-111111111111"}},
	}
	_, err := buildClientConnectionResponseForProtocol("account-id", subscription, ClientProfile{}, ClientProtocolVLESS)
	if !errors.Is(err, ErrClientConnectionUnavailable) || !strings.Contains(err.Error(), ErrNodeConfigNotApplied.Error()) {
		t.Fatalf("expected not-applied client connection error, got %v", err)
	}
}

func TestUseAppliedClientSettingsReplacesOnlyNodeEnforcedParameters(t *testing.T) {
	server := SubscriptionServer{
		PublicIP: "203.0.113.10", VLESSPort: 10443, VLESSNetwork: "tcp", RealityServerName: "saved.example.com",
		RealityPublicKey: "saved-pbk", RealityShortID: "saved-sid", WireGuardDNS: "9.9.9.9",
	}
	server.useAppliedClientSettings(platform.AppliedClientSettings{
		VLESSPort: 8443, RealityServerName: "www.microsoft.com", RealityPublicKey: "applied-pbk", RealityShortID: "applied-sid",
	})
	if server.VLESSPort != 8443 || server.RealityServerName != "www.microsoft.com" ||
		server.RealityPublicKey != "applied-pbk" || server.RealityShortID != "applied-sid" {
		t.Fatalf("applied node parameters were not used: %+v", server)
	}
	if server.PublicIP != "203.0.113.10" || server.VLESSNetwork != "tcp" || server.WireGuardDNS != "9.9.9.9" {
		t.Fatalf("client-only parameters must stay live: %+v", server)
	}

	server.useAppliedClientSettings(platform.AppliedClientSettings{})
	if server.VLESSPort != defaultSingBoxServerPort {
		t.Fatalf("missing applied VLESS port must use the default, got %d", server.VLESSPort)
	}
}
