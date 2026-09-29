package configs

import (
	"testing"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func TestAppliedClientSettingsCaptureRenderedNodeParameters(t *testing.T) {
	info := ServerConfigInfo{
		VPNProtocol: "vless", VLESSFlow: "xtls-rprx-vision",
		RealityPrivateKey: "private", RealityPublicKey: "public", RealityShortID: "0123456789abcdef", RealityServerName: "www.microsoft.com",
		WireGuardPort: 51820, WireGuardAddress: "10.66.0.1/24", WireGuardPublicKey: "wg-public", WireGuardDNS: "1.1.1.1",
		Hysteria2Port: 443, Hysteria2Domain: "hy.example.com",
		ShadowsocksPort: 8388, ShadowsocksServerKey: "ss-key",
		MTProtoPort: 9443, MTProtoSecret: "mt-secret",
	}
	want := platform.AppliedClientSettings{
		VPNProtocol: "vless", VLESSPort: defaultVLESSPort, VLESSFlow: "xtls-rprx-vision",
		RealityPublicKey: "public", RealityShortID: "0123456789abcdef", RealityServerName: "www.microsoft.com",
		WireGuardPort: 51820, WireGuardAddress: "10.66.0.1/24", WireGuardPublicKey: "wg-public",
		Hysteria2Port: 443, Hysteria2Domain: "hy.example.com",
		ShadowsocksPort: 8388, ShadowsocksServerKey: "ss-key",
		MTProtoPort: 9443, MTProtoSecret: "mt-secret",
	}
	if got := appliedClientSettings(info); got != want {
		t.Fatalf("applied client settings = %+v, want %+v", got, want)
	}
}
