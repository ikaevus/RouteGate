package vpnaccounts

import (
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderSubscriptionDeliveryAutoUsesBase64ShareLinkSubscription(t *testing.T) {
	connection := ClientConnectionResponse{
		Protocol: ClientProtocolVLESS,
		Connections: []ClientProtocolConnection{
			{Protocol: ClientProtocolVLESS, VLESSLink: "vless://user@example.invalid:443?security=reality"},
			{Protocol: ClientProtocolHysteria2, Hysteria2URI: "hysteria2://user:secret@example.invalid:8443/"},
		},
	}

	payload, err := renderSubscriptionDeliveryPayload(connection, SubscriptionProfile{}, SubscriptionDeliveryFormatAuto)
	if err != nil {
		t.Fatalf("render auto subscription: %v", err)
	}
	if payload.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type %q", payload.ContentType)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Body)
	if err != nil {
		t.Fatalf("decode subscription body: %v", err)
	}
	want := "vless://user@example.invalid:443?security=reality\nhysteria2://user:secret@example.invalid:8443/"
	if string(decoded) != want {
		t.Fatalf("unexpected decoded subscription:\n%s", decoded)
	}
	if strings.Join(payload.Protocols, ",") != "vless,hysteria2" {
		t.Fatalf("unexpected protocol list: %v", payload.Protocols)
	}
}

func TestRenderSubscriptionDeliveryAutoFallsBackToWireGuardConfig(t *testing.T) {
	connection := ClientConnectionResponse{
		Protocol:        ClientProtocolWireGuard,
		WireGuardConfig: "[Interface]\nPrivateKey = fixture\n",
	}

	payload, err := renderSubscriptionDeliveryPayload(connection, SubscriptionProfile{}, SubscriptionDeliveryFormatAuto)
	if err != nil {
		t.Fatalf("render WireGuard subscription: %v", err)
	}
	if payload.Filename != "routegate.conf" {
		t.Fatalf("unexpected filename %q", payload.Filename)
	}
	if payload.Body != connection.WireGuardConfig {
		t.Fatalf("unexpected WireGuard payload %q", payload.Body)
	}
}

func TestRenderSubscriptionDeliveryRawKeepsDirectURIAsCompatibilityFallback(t *testing.T) {
	connection := ClientConnectionResponse{
		Protocol:     ClientProtocolShadowsocks,
		ShadowsocksURI: "ss://fixture@example.invalid:8388",
	}

	payload, err := renderSubscriptionDeliveryPayload(connection, SubscriptionProfile{}, SubscriptionDeliveryFormatRaw)
	if err != nil {
		t.Fatalf("render raw subscription: %v", err)
	}
	if payload.Body != connection.ShadowsocksURI {
		t.Fatalf("unexpected raw payload %q", payload.Body)
	}
	if len(payload.Protocols) != 1 || payload.Protocols[0] != ClientProtocolShadowsocks {
		t.Fatalf("unexpected protocol list: %v", payload.Protocols)
	}
}

func TestRenderSubscriptionDeliveryRejectsUnsupportedFormat(t *testing.T) {
	_, err := renderSubscriptionDeliveryPayload(ClientConnectionResponse{}, SubscriptionProfile{}, "proprietary")
	if err == nil || !strings.Contains(err.Error(), "supported formats") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}

func TestSubscriptionDeliverySecurityHeadersPreventCachingAndReferrerLeakage(t *testing.T) {
	response := httptest.NewRecorder()
	setSubscriptionDeliverySecurityHeaders(response)

	if got := response.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("unexpected Cache-Control %q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("unexpected Referrer-Policy %q", got)
	}
	if got := response.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Fatalf("unexpected X-Robots-Tag %q", got)
	}
}
