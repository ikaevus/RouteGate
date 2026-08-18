package configs

import (
	"strings"
	"testing"
	"time"
)

const testHysteria2Password = "0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHysteria2AdapterRendersStrictUserpassAndACMEConfig(t *testing.T) {
	config := buildRenderedConfig(ServerConfigInfo{
		ID: "11111111-1111-1111-1111-111111111111", Name: "node", DeploymentRole: "vpn", Status: "active",
		VPNProtocol: "hysteria2", Hysteria2Port: 443, Hysteria2Domain: "vpn.example.com",
		Hysteria2ACMEEmail: "ops@example.com", Hysteria2MasqueradeURL: "https://www.cloudflare.com/",
		VPNAccounts: []VPNAccountConfigInfo{{
			ID: "22222222-2222-2222-2222-222222222222", DisplayName: "Alice", Status: "active",
			Hysteria2Password: testHysteria2Password, TrafficEnforcementStatus: "not_enforced",
		}},
	}, time.Unix(1, 0).UTC())
	if config.Metadata.VPNCore.Core != "hysteria" || config.Metadata.VPNCore.Transport != "quic" || config.Metadata.VPNCore.Security != "tls" {
		t.Fatalf("unexpected descriptor: %+v", config.Metadata.VPNCore)
	}
	parsed, err := parseHysteria2ServerConfig(config.Hysteria2)
	if err != nil { t.Fatalf("parse rendered Hysteria2 config: %v", err) }
	if parsed.ACME.Dir != hysteria2ACMEDir || parsed.Auth.Userpass["22222222-2222-2222-2222-222222222222"] != testHysteria2Password {
		t.Fatalf("unexpected Hysteria2 config: %+v", parsed)
	}
	if strings.Contains(config.Hysteria2, "privateKey") || !vpnServiceReady(config) {
		t.Fatalf("unexpected rendered readiness or key material: %s", config.Hysteria2)
	}
}

func TestHysteria2AdapterRejectsUnsafeMasquerade(t *testing.T) {
	config := RenderedConfig{Hysteria2: `{"listen":":443","acme":{"domains":["vpn.example.com"],"email":"ops@example.com","ca":"letsencrypt","dir":"/var/lib/hysteria/acme","type":"http"},"auth":{"type":"userpass","userpass":{"22222222-2222-2222-2222-222222222222":"0123456789abcdef0123456789abcdef0123456789abcdef"}},"masquerade":{"type":"proxy","proxy":{"url":"http://example.com","rewriteHost":true,"insecure":false,"xForwarded":false}}}`}
	if _, err := parseHysteria2ServerConfig(config.Hysteria2); err == nil { t.Fatal("expected unsafe masquerade rejection") }
}
