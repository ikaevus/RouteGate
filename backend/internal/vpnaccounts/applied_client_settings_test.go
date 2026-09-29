package vpnaccounts

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

const appliedTestUUID = "11111111-1111-4111-8111-111111111111"

func appliedTestSubscription(server SubscriptionServer) SubscriptionProfile {
	return SubscriptionProfile{
		Account:     Account{ID: "account-id", DisplayName: "Account", VLESSUUID: appliedTestUUID},
		Server:      &server,
		Credentials: SubscriptionCredentials{VLESS: VLESSCredentials{UUID: appliedTestUUID, Network: server.VLESSNetwork}},
	}
}

func vlessLinkQuery(t *testing.T, subscription SubscriptionProfile, profile ClientProfile) url.Values {
	t.Helper()
	response, err := buildClientConnectionResponseForProtocol("account-id", subscription, profile, ClientProtocolVLESS)
	if err != nil {
		t.Fatalf("build VLESS connection: %v", err)
	}
	parsed, err := url.Parse(response.VLESSLink)
	if err != nil {
		t.Fatalf("parse link %q: %v", response.VLESSLink, err)
	}
	return parsed.Query()
}

func TestClientConnectionUnavailableBeforeFirstApply(t *testing.T) {
	subscription := appliedTestSubscription(SubscriptionServer{
		PublicIP: "203.0.113.10", VLESSPort: 8443, RealityPublicKey: "pbk", RealityShortID: "sid",
		RealityServerName: "www.microsoft.com", AwaitingFirstApply: true,
	})
	_, err := buildClientConnectionResponseForProtocol("account-id", subscription, ClientProfile{}, ClientProtocolVLESS)
	if !errors.Is(err, ErrClientConnectionUnavailable) || !strings.Contains(err.Error(), ErrNodeConfigNotApplied.Error()) {
		t.Fatalf("expected not-applied client connection error, got %v", err)
	}
}

func TestUseAppliedClientSettingsReplacesNodeParameters(t *testing.T) {
	server := SubscriptionServer{
		PublicIP: "203.0.113.10", VLESSPort: 10443, VLESSNetwork: "ws", RealityServerName: "saved.example.com",
		RealityPublicKey: "saved-pbk", RealityShortID: "saved-sid", WireGuardDNS: "9.9.9.9",
		ShadowsocksMethod: "saved-method", MTProtoFrontingDomain: "saved.example.com",
	}
	server.useAppliedClientSettings(platform.AppliedClientSettings{
		VLESSPort: 8443, VLESSNetwork: "tcp", RealityServerName: "www.microsoft.com", RealityPublicKey: "applied-pbk",
		RealityShortID: "applied-sid", ShadowsocksMethod: "2022-blake3-aes-128-gcm", MTProtoFrontingDomain: "www.cloudflare.com",
	})
	if server.VLESSPort != 8443 || server.VLESSNetwork != "tcp" || server.RealityServerName != "www.microsoft.com" ||
		server.RealityPublicKey != "applied-pbk" || server.RealityShortID != "applied-sid" ||
		server.ShadowsocksMethod != "2022-blake3-aes-128-gcm" || server.MTProtoFrontingDomain != "www.cloudflare.com" {
		t.Fatalf("applied node parameters were not used: %+v", server)
	}
	// The endpoint and the DNS pushed to WireGuard clients are not part of the
	// node runtime and stay live.
	if server.PublicIP != "203.0.113.10" || server.WireGuardDNS != "9.9.9.9" {
		t.Fatalf("client-only parameters must stay live: %+v", server)
	}

	server.useAppliedClientSettings(platform.AppliedClientSettings{})
	if server.VLESSPort != defaultSingBoxServerPort {
		t.Fatalf("missing applied VLESS port must use the default, got %d", server.VLESSPort)
	}
}

// Saving a non-TCP VLESS transport never changes the node, which only serves
// raw TCP. Clients must keep receiving type=tcp.
func TestSavedVLESSTransportDoesNotReachClientLink(t *testing.T) {
	server := SubscriptionServer{
		PublicIP: "203.0.113.10", VLESSPort: 8443, VLESSNetwork: "ws", VLESSFlow: "xtls-rprx-vision",
		RealityPublicKey: "saved-pbk", RealityShortID: "sid", RealityServerName: "www.microsoft.com",
	}
	server.useAppliedClientSettings(platform.AppliedClientSettings{
		VLESSPort: 8443, VLESSNetwork: "tcp", VLESSFlow: "xtls-rprx-vision",
		RealityPublicKey: "applied-pbk", RealityShortID: "sid", RealityServerName: "www.microsoft.com",
	})
	query := vlessLinkQuery(t, appliedTestSubscription(server), ClientProfile{})
	if query.Get("type") != "tcp" || query.Get("pbk") != "applied-pbk" {
		t.Fatalf("client link must describe the applied TCP runtime, got type=%q pbk=%q", query.Get("type"), query.Get("pbk"))
	}
}

// A per-client SNI override that does not match the node's Reality server name
// (for example one left behind after the node was renamed) would break every
// handshake, so the link keeps the node's applied name.
func TestRealityServerNameOverrideCannotBreakClientLink(t *testing.T) {
	server := SubscriptionServer{
		PublicIP: "203.0.113.10", VLESSPort: 8443, VLESSNetwork: "tcp",
		RealityPublicKey: "pbk", RealityShortID: "sid", RealityServerName: "www.microsoft.com",
	}
	subscription := appliedTestSubscription(server)
	if sni := vlessLinkQuery(t, subscription, ClientProfile{ServerNameOverride: "old-name.example.com"}).Get("sni"); sni != "www.microsoft.com" {
		t.Fatalf("mismatching override reached the client: sni=%q", sni)
	}
	if sni := vlessLinkQuery(t, subscription, ClientProfile{ServerNameOverride: "WWW.Microsoft.com"}).Get("sni"); sni != "WWW.Microsoft.com" {
		t.Fatalf("matching override must be kept, got sni=%q", sni)
	}
}
