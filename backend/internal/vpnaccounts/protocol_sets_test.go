package vpnaccounts

import (
	"reflect"
	"testing"
)

func TestNormalizeConcreteClientProtocolsDeduplicatesInCanonicalOrder(t *testing.T) {
	got, err := normalizeConcreteClientProtocols([]string{
		"shadowsocks",
		" VLESS ",
		"wireguard",
		"vless",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ClientProtocolVLESS, ClientProtocolWireGuard, ClientProtocolShadowsocks}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("protocols = %#v, want %#v", got, want)
	}
}

func TestNormalizeConcreteClientProtocolsRejectsAuto(t *testing.T) {
	if _, err := normalizeConcreteClientProtocols([]string{ClientProtocolAuto}); err == nil {
		t.Fatal("expected auto protocol to be rejected from enabledProtocols")
	}
}

func TestEffectiveRequestedProtocolsFallsBackToPrimary(t *testing.T) {
	profile := ClientProfile{Protocol: ClientProtocolWireGuard}
	got := effectiveRequestedProtocols(profile, nil)
	want := []string{ClientProtocolWireGuard}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("protocols = %#v, want %#v", got, want)
	}
}
