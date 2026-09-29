package configs

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/platform"
	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

func renderedClientSettingsFixture(t *testing.T, savedRealityPublicKey, savedNetwork string) (ServerConfigInfo, string, string) {
	t.Helper()
	realityKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverWG, err := wgcredentials.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	peerWG, err := wgcredentials.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	realityPublic := base64.RawURLEncoding.EncodeToString(realityKey.PublicKey().Bytes())
	if savedRealityPublicKey == "" {
		savedRealityPublicKey = realityPublic
	}
	info := ServerConfigInfo{
		ID: "server-id", Name: "RU", DeploymentRole: string(platform.DeploymentRoleVPN),
		VPNProtocol: platform.VPNProtocolVLESS, VLESSPort: 8443, VLESSFlow: "xtls-rprx-vision", VLESSNetwork: savedNetwork,
		RealityPrivateKey: base64.RawURLEncoding.EncodeToString(realityKey.Bytes()), RealityPublicKey: savedRealityPublicKey,
		RealityShortID: "0123456789abcdef", RealityServerName: "www.microsoft.com",
		WireGuardPort: 51820, WireGuardAddress: "10.66.0.1/24", WireGuardDNS: "9.9.9.9",
		WireGuardPrivateKey: serverWG.PrivateKey, WireGuardPublicKey: serverWG.PublicKey,
		ShadowsocksPort: 8388, ShadowsocksServerKey: "c2VydmVyLWtleS0xNmJ5dGU=",
		VPNAccounts: []VPNAccountConfigInfo{{
			ID: "account-1", DisplayName: "Account", Status: "active", TrafficEnforcementStatus: "normal",
			VPNProtocol:  platform.VPNProtocolVLESS,
			VPNProtocols: []string{platform.VPNProtocolVLESS, platform.VPNProtocolWireGuard, platform.VPNProtocolShadowsocks},
			VLESSUUID:    "11111111-1111-4111-8111-111111111111", VLESSFlow: "xtls-rprx-vision",
			WireGuardPublicKey: peerWG.PublicKey, WireGuardAddress: "10.66.0.2/32",
			ShadowsocksUserKey: "dXNlci1rZXktMTZieXRlcw==",
		}},
	}
	return info, realityPublic, serverWG.PublicKey
}

func renderedJSON(t *testing.T, info ServerConfigInfo) []byte {
	t.Helper()
	payload, err := json.Marshal(buildRenderedConfig(info, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestClientSettingsAreDerivedFromTheRenderedRuntime(t *testing.T) {
	info, realityPublic, wireGuardPublic := renderedClientSettingsFixture(t, "", "")
	got, err := clientSettingsFromRenderedJSON(renderedJSON(t, info), nil)
	if err != nil {
		t.Fatalf("derive client settings: %v", err)
	}
	want := platform.AppliedClientSettings{
		VPNProtocol: platform.VPNProtocolVLESS, VLESSPort: 8443, VLESSFlow: "xtls-rprx-vision", VLESSNetwork: "tcp",
		RealityPublicKey: realityPublic, RealityShortID: "0123456789abcdef", RealityServerName: "www.microsoft.com",
		WireGuardPort: 51820, WireGuardAddress: "10.66.0.1/24", WireGuardPublicKey: wireGuardPublic,
		ShadowsocksPort: 8388, ShadowsocksMethod: shadowsocksMethod, ShadowsocksServerKey: "c2VydmVyLWtleS0xNmJ5dGU=",
		Accounts: map[string]platform.AppliedAccountProtocols{"account-1": {
			Protocols: []string{platform.VPNProtocolVLESS, platform.VPNProtocolWireGuard, platform.VPNProtocolShadowsocks},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("client settings = %+v\nwant %+v", got, want)
	}
}

// Values the node runtime ignores must not reach clients: the managed VLESS
// inbound always serves raw TCP, and the Reality public key clients need is
// the one matching the private key actually deployed.
func TestClientSettingsIgnoreSavedValuesTheNodeDoesNotServe(t *testing.T) {
	for _, network := range []string{"ws", "grpc", "http"} {
		info, realityPublic, _ := renderedClientSettingsFixture(t, "stale-public-key", network)
		got, err := clientSettingsFromRenderedJSON(renderedJSON(t, info), nil)
		if err != nil {
			t.Fatalf("derive client settings: %v", err)
		}
		if got.VLESSNetwork != platform.VPNTransportTCP {
			t.Fatalf("saved VLESS network %q reached clients as %q", network, got.VLESSNetwork)
		}
		if got.RealityPublicKey != realityPublic {
			t.Fatalf("Reality public key = %q, want the key derived from the deployed private key", got.RealityPublicKey)
		}
	}
}

func TestClientSettingsRejectUnparseableProtocolSections(t *testing.T) {
	info, _, _ := renderedClientSettingsFixture(t, "", "")
	rendered := buildRenderedConfig(info, time.Now())
	rendered.WireGuard = "[Interface]\nPrivateKey = broken\n"
	payload, err := json.Marshal(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientSettingsFromRenderedJSON(payload, nil); err == nil {
		t.Fatal("a broken protocol section must not yield a partial snapshot")
	}
	if snapshot := clientSettingsSnapshot(payload, nil); snapshot != nil {
		t.Fatalf("broken rendered config produced snapshot %s", snapshot)
	}
}

func TestClientSettingsCoverHysteria2AndMTProto(t *testing.T) {
	secret := "ee" + "00112233445566778899aabbccddeeff" + "7777772e636c6f7564666c6172652e636f6d"
	info := ServerConfigInfo{
		ID: "server-id", Name: "FI", DeploymentRole: string(platform.DeploymentRoleVPN),
		VPNProtocol:   platform.VPNProtocolHysteria2,
		Hysteria2Port: 443, Hysteria2Domain: "hy.example.com", Hysteria2ACMEEmail: "ops@example.com",
		Hysteria2MasqueradeURL: "https://www.cloudflare.com/",
		MTProtoPort:            9443, MTProtoSecret: secret, MTProtoFrontingDomain: "www.cloudflare.com",
		VPNAccounts: []VPNAccountConfigInfo{{
			ID: "account-1", DisplayName: "Account", Status: "active", TrafficEnforcementStatus: "normal",
			VPNProtocol:       platform.VPNProtocolHysteria2,
			VPNProtocols:      []string{platform.VPNProtocolHysteria2, platform.VPNProtocolMTProto},
			Hysteria2Password: "hysteria-password",
		}},
	}
	got, err := clientSettingsFromRenderedJSON(renderedJSON(t, info), nil)
	if err != nil {
		t.Fatalf("derive client settings: %v", err)
	}
	if got.Hysteria2Port != 443 || got.Hysteria2Domain != "hy.example.com" ||
		got.MTProtoPort != 9443 || got.MTProtoSecret != secret || got.MTProtoFrontingDomain != "www.cloudflare.com" {
		t.Fatalf("client settings = %+v", got)
	}
}

// The rendered JSON records only each account's protocol set; the primary
// chosen at render time is kept only when the version deploys it.
func TestClientSettingsRecordRenderedAccountPrimaryProtocol(t *testing.T) {
	info, _, _ := renderedClientSettingsFixture(t, "", "")
	payload := renderedJSON(t, info)
	got, err := clientSettingsFromRenderedJSON(payload, map[string]string{"account-1": platform.VPNProtocolShadowsocks})
	if err != nil {
		t.Fatalf("derive client settings: %v", err)
	}
	if account := got.Accounts["account-1"]; account.Primary != platform.VPNProtocolShadowsocks {
		t.Fatalf("primary = %q, want shadowsocks", account.Primary)
	}
	got, err = clientSettingsFromRenderedJSON(payload, map[string]string{"account-1": platform.VPNProtocolHysteria2})
	if err != nil {
		t.Fatalf("derive client settings: %v", err)
	}
	if account := got.Accounts["account-1"]; account.Primary != "" {
		t.Fatalf("a primary the version does not deploy must be dropped, got %q", account.Primary)
	}
}
