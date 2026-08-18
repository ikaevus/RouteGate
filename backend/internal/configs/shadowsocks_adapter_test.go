package configs

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func TestShadowsocksAdapterRendersStrictAEAD2022MultiUserConfig(t *testing.T) {
	serverKey := base64.StdEncoding.EncodeToString([]byte("server-key-16byt"))
	userKey := base64.StdEncoding.EncodeToString([]byte("user-key-16-byts"))
	config := buildRenderedConfig(ServerConfigInfo{
		ID: "server-id", Name: "ss-01", Status: "active", VPNProtocol: platform.VPNProtocolShadowsocks,
		ShadowsocksPort: 8388, ShadowsocksMethod: shadowsocksMethod, ShadowsocksServerKey: serverKey,
		VPNAccounts: []VPNAccountConfigInfo{{
			ID: "11111111-1111-1111-1111-111111111111", DisplayName: "Alice", Status: "active",
			ShadowsocksUserKey: userKey,
		}},
	}, time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC))

	if config.Metadata.VPNCore.Core != platform.VPNCoreSingBox || config.Metadata.VPNCore.Protocol != platform.VPNProtocolShadowsocks ||
		config.Metadata.VPNCore.Transport != platform.VPNTransportTCP || config.Metadata.VPNCore.Security != platform.VPNSecurityAEAD2022 {
		t.Fatalf("unexpected Shadowsocks descriptor: %+v", config.Metadata.VPNCore)
	}
	if len(config.SingBox.Inbounds) != 1 {
		t.Fatalf("inbound count = %d, want 1", len(config.SingBox.Inbounds))
	}
	inbound := config.SingBox.Inbounds[0]
	if inbound["method"] != shadowsocksMethod || inbound["network"] != "tcp" || inbound["password"] != serverKey {
		t.Fatalf("unexpected Shadowsocks inbound: %+v", inbound)
	}
	if result := ValidateRenderedConfig(config); !result.Valid {
		t.Fatalf("Shadowsocks config should validate: %+v", result)
	}
	if !vpnServiceReady(config) {
		t.Fatal("Shadowsocks config with one user should be apply-ready")
	}
}

func TestShadowsocksAdapterRejectsNonAEAD2022Method(t *testing.T) {
	serverKey := base64.StdEncoding.EncodeToString([]byte("server-key-16byt"))
	userKey := base64.StdEncoding.EncodeToString([]byte("user-key-16-byts"))
	config := buildRenderedConfig(ServerConfigInfo{
		ID: "server-id", Name: "ss-01", Status: "active", VPNProtocol: platform.VPNProtocolShadowsocks,
		ShadowsocksPort: 8388, ShadowsocksServerKey: serverKey,
		VPNAccounts: []VPNAccountConfigInfo{{ID: "11111111-1111-1111-1111-111111111111", Status: "active", ShadowsocksUserKey: userKey}},
	}, time.Now().UTC())
	config.SingBox.Inbounds[0]["method"] = "aes-256-gcm"
	result := ValidateRenderedConfig(config)
	if result.Valid || !strings.Contains(strings.Join(result.Errors, " "), "AEAD-2022") {
		t.Fatalf("expected strict method rejection, got %+v", result)
	}
}
