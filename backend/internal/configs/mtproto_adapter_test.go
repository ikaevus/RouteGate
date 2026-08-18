package configs

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func testMTProtoSecret() string {
	return "ee" + strings.Repeat("ab", 16) + hex.EncodeToString([]byte(mtprotoFrontingDomain))
}

func TestMTProtoAdapterRendersStrictMTGConfig(t *testing.T) {
	config := buildRenderedConfig(ServerConfigInfo{
		ID: "server-id", Name: "mtproto-01", Status: "active", VPNProtocol: platform.VPNProtocolMTProto,
		MTProtoPort: 8443, MTProtoSecret: testMTProtoSecret(), MTProtoFrontingDomain: mtprotoFrontingDomain,
	}, time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC))

	if config.Metadata.VPNCore.Core != platform.VPNCoreMTG || config.Metadata.VPNCore.Protocol != platform.VPNProtocolMTProto ||
		config.Metadata.VPNCore.Transport != platform.VPNTransportTCP || config.Metadata.VPNCore.Security != platform.VPNSecurityFakeTLS {
		t.Fatalf("unexpected MTProto descriptor: %+v", config.Metadata.VPNCore)
	}
	parsed, err := parseMTProtoServerConfig(config.MTProto)
	if err != nil {
		t.Fatalf("parse rendered MTProto config: %v", err)
	}
	if parsed.Port != 8443 || parsed.Secret != testMTProtoSecret() || parsed.FrontingDomain != mtprotoFrontingDomain {
		t.Fatalf("unexpected MTProto config: %+v", parsed)
	}
	if result := ValidateRenderedConfig(config); !result.Valid {
		t.Fatalf("MTProto config should validate: %+v", result)
	}
	if !vpnServiceReady(config) {
		t.Fatal("valid node-level MTProto config should be apply-ready")
	}
}

func TestMTProtoAdapterRejectsArbitraryTOML(t *testing.T) {
	config := buildRenderedConfig(ServerConfigInfo{
		ID: "server-id", Name: "mtproto-01", Status: "active", VPNProtocol: platform.VPNProtocolMTProto,
		MTProtoSecret: testMTProtoSecret(),
	}, time.Now().UTC())
	config.MTProto += "network.proxies = [\"socks5://127.0.0.1:1080\"]\n"
	result := ValidateRenderedConfig(config)
	if result.Valid || !strings.Contains(strings.Join(result.Errors, " "), "not allowed") {
		t.Fatalf("expected arbitrary TOML rejection, got %+v", result)
	}
}
