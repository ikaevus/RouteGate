package delivery

import (
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func TestBuildProtocolAccessBundleCoversSupportedProtocols(t *testing.T) {
	cases := []struct {
		name       string
		connection vpnaccounts.ClientConnectionResponse
		assert     func(t *testing.T, bundle ProtocolAccessBundle)
	}{
		{
			name: "vless",
			connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolVLESS, VLESSLink: "vless://fixture@example.invalid:443", Profile: vpnaccounts.ClientProfile{Name: "Phone"}},
			assert: func(t *testing.T, bundle ProtocolAccessBundle) {
				if bundle.URI == "" || bundle.QRPayload != bundle.URI || bundle.ConfigText != "" {
					t.Fatalf("unexpected VLESS bundle: %+v", bundle)
				}
			},
		},
		{
			name: "shadowsocks",
			connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolShadowsocks, ShadowsocksURI: "ss://fixture@example.invalid:8388/#RouteGate", Profile: vpnaccounts.ClientProfile{Name: "Phone"}},
			assert: func(t *testing.T, bundle ProtocolAccessBundle) {
				if !strings.HasPrefix(bundle.URI, "ss://") || bundle.QRPayload != bundle.URI {
					t.Fatalf("unexpected Shadowsocks bundle: %+v", bundle)
				}
			},
		},
		{
			name: "wireguard",
			connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolWireGuard, WireGuardConfig: "[Interface]\nPrivateKey = fixture\n\n[Peer]\nPublicKey = fixture\nEndpoint = example.invalid:51820\n", Profile: vpnaccounts.ClientProfile{Name: "Felix Phone"}},
			assert: func(t *testing.T, bundle ProtocolAccessBundle) {
				if bundle.URI != "" || bundle.ConfigText == "" || bundle.QRPayload != bundle.ConfigText || bundle.ConfigFilename != "felix-phone.conf" {
					t.Fatalf("unexpected WireGuard bundle: %+v", bundle)
				}
			},
		},
		{
			name: "mtproto",
			connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolMTProto, MTProtoURI: "tg://proxy?server=example.invalid&port=8443&secret=eefixture", Profile: vpnaccounts.ClientProfile{Name: "Telegram"}},
			assert: func(t *testing.T, bundle ProtocolAccessBundle) {
				if !strings.HasPrefix(bundle.URI, "tg://proxy?") || !strings.HasPrefix(bundle.AlternativeURI, "https://t.me/proxy?") {
					t.Fatalf("unexpected MTProto bundle: %+v", bundle)
				}
			},
		},
		{
			name: "hysteria2",
			connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolHysteria2, Hysteria2URI: "hysteria2://fixture@example.invalid:443/?sni=example.invalid", Profile: vpnaccounts.ClientProfile{Name: "Laptop"}},
			assert: func(t *testing.T, bundle ProtocolAccessBundle) {
				if !strings.HasPrefix(bundle.URI, "hysteria2://") || bundle.QRPayload != bundle.URI {
					t.Fatalf("unexpected Hysteria2 bundle: %+v", bundle)
				}
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bundle, err := buildProtocolAccessBundle(test.connection, "en")
			if err != nil {
				t.Fatalf("build bundle: %v", err)
			}
			if bundle.Protocol != test.connection.Protocol || bundle.DisplayName == "" || bundle.PrimaryAction == "" {
				t.Fatalf("incomplete bundle: %+v", bundle)
			}
			test.assert(t, bundle)
		})
	}
}

func TestProtocolAwareTemplatesAndBrandingRenderInEnglishAndRussian(t *testing.T) {
	renderer := NewRenderer()
	for _, locale := range []string{"en", "ru"} {
		message, err := renderer.Render(TemplateVPNAccess, locale, TemplateData{
			ProfileName: "Phone",
			Access: ProtocolAccessBundle{
				Protocol:      vpnaccounts.ClientProtocolWireGuard,
				DisplayName:   "WireGuard",
				PrimaryAction: "Import configuration",
				ConfigText:    "[Interface]\nPrivateKey = fixture",
				ClientHint:    "WireGuard hint",
			},
			Branding: DefaultDeliveryBranding(locale),
		})
		if err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		if !strings.Contains(message.Text, "WireGuard") || !strings.Contains(message.Text, "PrivateKey = fixture") || !strings.Contains(message.Text, "routegate.org") {
			t.Fatalf("protocol-aware text missing data for %s: %q", locale, message.Text)
		}
		if !strings.Contains(message.HTML, "routegate-symbol.svg") || !strings.Contains(message.HTML, "WireGuard") {
			t.Fatalf("HTML branding/access missing for %s: %q", locale, message.HTML)
		}
	}
}

func TestDefaultDeliveryBrandingCanBeDisabledExplicitly(t *testing.T) {
	text, htmlBody := appendDeliveryBranding("body", "<p>body</p>", DeliveryBranding{ShowBranding: false})
	if text != "body" || htmlBody != "<p>body</p>" {
		t.Fatalf("disabled branding changed message: %q %q", text, htmlBody)
	}
}
