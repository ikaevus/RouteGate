package vpnaccounts

import (
	"strings"
	"testing"
)

func TestRenderWireGuardClientConfigNormalizesHostPrefixDNS(t *testing.T) {
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	config, err := RenderWireGuardClientConfig(SubscriptionProfile{
		Server: &SubscriptionServer{
			PublicIP:          "203.0.113.10",
			VPNProtocol:       "wireguard",
			WireGuardPort:     51820,
			WireGuardDNS:      "1.1.1.1/32",
			WireGuardPublicKey: key,
		},
		Credentials: SubscriptionCredentials{WireGuard: WireGuardCredentials{
			PrivateKey: key,
			PublicKey:  key,
			Address:    "10.66.0.2/32",
		}},
	})
	if err != nil {
		t.Fatalf("render WireGuard client config: %v", err)
	}
	if !strings.Contains(config, "DNS = 1.1.1.1") {
		t.Fatalf("expected normalized DNS address, got:\n%s", config)
	}
	if strings.Contains(config, "DNS = 1.1.1.1/32") {
		t.Fatalf("expected DNS prefix to be normalized, got:\n%s", config)
	}
}
