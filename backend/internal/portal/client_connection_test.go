package portal

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type portalClientConnectionSource struct {
	subscription vpnaccounts.SubscriptionProfile
	profile      vpnaccounts.ClientProfile
	subscriptionReads int
	profileReads      int
}

func (s *portalClientConnectionSource) GetSubscriptionProfileByAccountID(context.Context, string) (vpnaccounts.SubscriptionProfile, error) {
	s.subscriptionReads++
	return s.subscription, nil
}

func (s *portalClientConnectionSource) GetOrCreateClientProfile(context.Context, string) (vpnaccounts.ClientProfile, error) {
	s.profileReads++
	return s.profile, nil
}

func TestBuildPortalDirectQRCodeUsesWireGuardConnectionMaterial(t *testing.T) {
	privateKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	serverPublicKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	source := &portalClientConnectionSource{
		subscription: vpnaccounts.SubscriptionProfile{
			Account: vpnaccounts.Account{ID: "account-1", DisplayName: "Phone", Status: vpnaccounts.StatusActive, ServerID: "server-1"},
			Server: &vpnaccounts.SubscriptionServer{
				ID: "server-1", Name: "VPN Node", PublicIP: "203.0.113.10", VPNProtocol: vpnaccounts.ClientProtocolWireGuard,
				WireGuardPort: 51820, WireGuardDNS: "1.1.1.1", WireGuardPublicKey: serverPublicKey,
			},
			Credentials: vpnaccounts.SubscriptionCredentials{
				WireGuard: vpnaccounts.WireGuardCredentials{PrivateKey: privateKey, Address: "10.66.0.2"},
			},
		},
		profile: vpnaccounts.ClientProfile{
			VPNAccountID: "account-1", Name: "Phone", Protocol: vpnaccounts.ClientProtocolWireGuard,
			FingerprintMode: vpnaccounts.FingerprintModeAuto, Fingerprint: vpnaccounts.DefaultAutoFingerprint,
		},
	}

	qr, err := buildPortalDirectQRCode(context.Background(), source, PortalProfile{
		ID: "account-1", AccessStatus: AccessStatusActive, Protocol: "wireguard",
	}, "en")
	if err != nil {
		t.Fatalf("build direct QR: %v", err)
	}
	if !qr.Available || qr.Format != "wireguard-config" {
		t.Fatalf("unexpected QR metadata: %+v", qr)
	}
	if !strings.Contains(qr.QRText, "[Interface]") || !strings.Contains(qr.QRText, "PrivateKey = "+privateKey) || !strings.Contains(qr.QRText, "[Peer]") {
		t.Fatalf("QR does not contain WireGuard client config: %q", qr.QRText)
	}
	if strings.Contains(qr.QRText, "/api/v1/subscriptions/") {
		t.Fatalf("direct QR unexpectedly contains a subscription URL: %q", qr.QRText)
	}
	if source.profileReads != 1 {
		t.Fatalf("client profile reads = %d, want 1", source.profileReads)
	}
}

func TestBuildPortalDirectQRCodeDoesNotResolveInactiveProfile(t *testing.T) {
	source := &portalClientConnectionSource{}
	qr, err := buildPortalDirectQRCode(context.Background(), source, PortalProfile{
		ID: "account-1", AccessStatus: AccessStatusSuspended,
	}, "en")
	if err != nil {
		t.Fatalf("inactive QR: %v", err)
	}
	if qr.Available || qr.QRText != "" || source.subscriptionReads != 0 || source.profileReads != 0 {
		t.Fatalf("inactive profile unexpectedly resolved connection material: qr=%+v subscription_reads=%d profile_reads=%d", qr, source.subscriptionReads, source.profileReads)
	}
}

func TestPortalConnectionMaterialSelectsProtocolSpecificField(t *testing.T) {
	cases := []struct {
		name       string
		connection vpnaccounts.ClientConnectionResponse
		want       string
	}{
		{"vless", vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolVLESS, VLESSLink: "vless://fixture"}, "vless://fixture"},
		{"wireguard", vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolWireGuard, WireGuardConfig: "[Interface]\nPrivateKey = fixture\n"}, "[Interface]\nPrivateKey = fixture\n"},
		{"hysteria2", vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolHysteria2, Hysteria2URI: "hysteria2://fixture"}, "hysteria2://fixture"},
		{"shadowsocks", vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolShadowsocks, ShadowsocksURI: "ss://fixture"}, "ss://fixture"},
		{"mtproto", vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolMTProto, MTProtoURI: "tg://proxy?fixture"}, "tg://proxy?fixture"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := portalConnectionMaterial(test.connection); got != test.want {
				t.Fatalf("material=%q want=%q", got, test.want)
			}
		})
	}
}


func TestBuildPortalDirectQRCodeWithoutConnectionSourceIsUnavailable(t *testing.T) {
	qr, err := buildPortalDirectQRCode(context.Background(), nil, PortalProfile{
		ID: "account-1", AccessStatus: AccessStatusActive,
	}, "en")
	if err != nil {
		t.Fatalf("active QR without source: %v", err)
	}
	if qr.Available || qr.QRText != "" || qr.Message != localizedQRCodeNotReady("en") {
		t.Fatalf("expected unavailable QR metadata, got %+v", qr)
	}
}

func TestPortalConnectionMaterialRejectsUnknownProtocolAndEmptyPayload(t *testing.T) {
	if got := portalConnectionMaterial(vpnaccounts.ClientConnectionResponse{
		Protocol: "unknown", VLESSLink: "vless://must-not-be-used",
	}); got != "" {
		t.Fatalf("unknown protocol material=%q, want empty", got)
	}
	if got := portalConnectionMaterial(vpnaccounts.ClientConnectionResponse{
		Protocol: vpnaccounts.ClientProtocolVLESS, VLESSLink: "   ",
	}); got != "" {
		t.Fatalf("empty VLESS material=%q, want empty", got)
	}
}
