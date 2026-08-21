package delivery

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func TestBuildProtocolConnectURLKeepsEveryCredentialInFragment(t *testing.T) {
	cases := []struct {
		protocol string
		material string
	}{
		{protocol: "vless", material: "vless://00000000-0000-0000-0000-000000000000@example.invalid:8443?security=reality"},
		{protocol: "wireguard", material: "[Interface]\nPrivateKey = private-fixture\nAddress = 10.66.0.2/32\n\n[Peer]\nPublicKey = public-fixture\nEndpoint = example.invalid:51820\n"},
		{protocol: "hysteria2", material: "hysteria2://user:password@example.invalid:443/?sni=example.invalid"},
		{protocol: "shadowsocks", material: "ss://fixture@example.invalid:8388/#RouteGate"},
		{protocol: "mtproto", material: "tg://proxy?server=example.invalid&port=8443&secret=eefixture"},
	}

	for _, test := range cases {
		t.Run(test.protocol, func(t *testing.T) {
			connectURL, err := BuildProtocolConnectURL("https://vpn.example.com/", test.protocol, test.material)
			if err != nil {
				t.Fatalf("build connect URL: %v", err)
			}
			parsed, err := url.Parse(connectURL)
			if err != nil {
				t.Fatalf("parse connect URL: %v", err)
			}
			if parsed.Scheme != "https" || parsed.Host != "vpn.example.com" || parsed.Path != "/connect.html" || parsed.RawQuery != "" {
				t.Fatalf("unexpected connect URL: %q", connectURL)
			}
			if strings.Contains(parsed.Path, test.material) || strings.Contains(parsed.RawQuery, test.material) {
				t.Fatalf("access material escaped into request URL: %q", connectURL)
			}
			fragment, err := url.ParseQuery(parsed.Fragment)
			if err != nil {
				t.Fatalf("parse fragment: %v", err)
			}
			if fragment.Get("protocol") != test.protocol {
				t.Fatalf("protocol=%q want=%q", fragment.Get("protocol"), test.protocol)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(fragment.Get("profile"))
			if err != nil {
				t.Fatalf("decode profile: %v", err)
			}
			if string(decoded) != test.material {
				t.Fatalf("decoded material mismatch: %q", string(decoded))
			}
		})
	}
}

func TestClientConnectionAccessMaterialUsesEffectiveProtocolPayload(t *testing.T) {
	cases := []struct {
		name       string
		connection vpnaccounts.ClientConnectionResponse
		want       string
	}{
		{name: "vless", connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolVLESS, VLESSLink: "vless://fixture@example.invalid:8443"}, want: "vless://fixture@example.invalid:8443"},
		{name: "wireguard", connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolWireGuard, WireGuardConfig: "[Interface]\nPrivateKey = fixture\n\n[Peer]\nPublicKey = fixture\n"}, want: "[Interface]\nPrivateKey = fixture\n\n[Peer]\nPublicKey = fixture\n"},
		{name: "hysteria2", connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolHysteria2, Hysteria2URI: "hysteria2://fixture@example.invalid:443"}, want: "hysteria2://fixture@example.invalid:443"},
		{name: "shadowsocks", connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolShadowsocks, ShadowsocksURI: "ss://fixture@example.invalid:8388/"}, want: "ss://fixture@example.invalid:8388/"},
		{name: "mtproto", connection: vpnaccounts.ClientConnectionResponse{Protocol: vpnaccounts.ClientProtocolMTProto, MTProtoURI: "tg://proxy?server=example.invalid&port=8443&secret=fixture"}, want: "tg://proxy?server=example.invalid&port=8443&secret=fixture"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			protocol, material, err := clientConnectionAccessMaterial(test.connection)
			if err != nil {
				t.Fatalf("resolve access material: %v", err)
			}
			if protocol != test.connection.Protocol || material != test.want {
				t.Fatalf("protocol=%q material=%q", protocol, material)
			}
		})
	}
}

func TestClientConnectionAccessMaterialRejectsMissingProtocolPayload(t *testing.T) {
	_, _, err := clientConnectionAccessMaterial(vpnaccounts.ClientConnectionResponse{
		Protocol: vpnaccounts.ClientProtocolWireGuard,
		VLESSLink: "vless://wrong-field@example.invalid:8443",
	})
	if err == nil {
		t.Fatal("expected missing WireGuard material to be rejected")
	}
	failure, ok := err.(Failure)
	if !ok || failure.Class != ErrorClassPermanent || failure.Code != "access_material_invalid" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestAccessMaterialErrorClassificationKeepsSpecificDiagnostics(t *testing.T) {
	endpoint := classifyAccessMaterialError(fmt.Errorf("%w: server endpoint is required", vpnaccounts.ErrClientConnectionUnavailable))
	if endpoint.Class != ErrorClassPermanent || endpoint.Code != "vpn_endpoint_missing" {
		t.Fatalf("endpoint classification = %+v", endpoint)
	}

	topology := classifyAccessMaterialError(fmt.Errorf("%w: Hysteria2 requires a dedicated VPN Node", vpnaccounts.ErrClientConnectionUnavailable))
	if topology.Class != ErrorClassPermanent || topology.Code != "vpn_access_incomplete" {
		t.Fatalf("topology classification = %+v", topology)
	}
}
