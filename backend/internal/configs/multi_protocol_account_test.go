package configs

import (
	"reflect"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func TestAccountUsesEveryEnabledProtocol(t *testing.T) {
	account := VPNAccountConfigInfo{
		VPNProtocol:  platform.VPNProtocolVLESS,
		VPNProtocols: []string{platform.VPNProtocolVLESS, platform.VPNProtocolWireGuard, platform.VPNProtocolShadowsocks},
	}

	for _, protocol := range []string{
		platform.VPNProtocolVLESS,
		platform.VPNProtocolWireGuard,
		platform.VPNProtocolShadowsocks,
	} {
		if !accountUsesProtocol(account, protocol) {
			t.Fatalf("expected account to use %s", protocol)
		}
	}
	if accountUsesProtocol(account, platform.VPNProtocolMTProto) {
		t.Fatal("account unexpectedly uses mtproto")
	}
}

func TestSelectedVPNCoreAdaptersIncludesMultipleProtocolsForSameAccount(t *testing.T) {
	info := ServerConfigInfo{
		VPNProtocol: platform.VPNProtocolVLESS,
		VPNAccounts: []VPNAccountConfigInfo{{
			ID:                       "account-1",
			Status:                   "active",
			TrafficEnforcementStatus: "normal",
			VPNProtocol:              platform.VPNProtocolVLESS,
			VPNProtocols: []string{
				platform.VPNProtocolVLESS,
				platform.VPNProtocolWireGuard,
				platform.VPNProtocolShadowsocks,
			},
		}},
	}

	adapters := selectedVPNCoreAdapters(info)
	got := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		got = append(got, adapter.Descriptor().Protocol)
	}
	want := []string{
		platform.VPNProtocolVLESS,
		platform.VPNProtocolWireGuard,
		platform.VPNProtocolShadowsocks,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter protocols = %#v, want %#v", got, want)
	}
}

func TestApplyResolvedAccountProtocolsKeepsPrimaryAndSet(t *testing.T) {
	info := ServerConfigInfo{
		VPNProtocol: platform.VPNProtocolVLESS,
		VPNAccounts: []VPNAccountConfigInfo{{ID: "account-1"}},
	}
	resolved := map[string]accountProtocolSelection{
		"account-1": {
			Primary:   platform.VPNProtocolWireGuard,
			Protocols: []string{platform.VPNProtocolVLESS, platform.VPNProtocolWireGuard},
		},
	}

	applyResolvedAccountProtocols(&info, resolved)
	account := info.VPNAccounts[0]
	if account.VPNProtocol != platform.VPNProtocolWireGuard {
		t.Fatalf("primary protocol = %q", account.VPNProtocol)
	}
	want := []string{platform.VPNProtocolVLESS, platform.VPNProtocolWireGuard}
	if !reflect.DeepEqual(account.VPNProtocols, want) {
		t.Fatalf("protocols = %#v, want %#v", account.VPNProtocols, want)
	}
}

func TestMergeRenderedVPNAccountsPreservesProtocolMaterials(t *testing.T) {
	config := RenderedConfig{VPNAccounts: []ConfigVPNAccount{
		{ID: "account-1", DisplayName: "Alice", Status: "active", VLESSUUID: "uuid"},
		{ID: "account-1", DisplayName: "Alice", Status: "active", Protocol: platform.VPNProtocolWireGuard, WireGuardPublicKey: "pub", WireGuardAddress: "10.70.0.2"},
		{ID: "account-1", DisplayName: "Alice", Status: "active", Protocol: platform.VPNProtocolShadowsocks, ShadowsocksUsername: "alice"},
	}}

	mergeRenderedVPNAccounts(&config)
	if len(config.VPNAccounts) != 1 {
		t.Fatalf("merged accounts = %d, want 1", len(config.VPNAccounts))
	}
	account := config.VPNAccounts[0]
	wantProtocols := []string{
		platform.VPNProtocolVLESS,
		platform.VPNProtocolWireGuard,
		platform.VPNProtocolShadowsocks,
	}
	if !reflect.DeepEqual(account.Protocols, wantProtocols) {
		t.Fatalf("protocols = %#v, want %#v", account.Protocols, wantProtocols)
	}
	if account.VLESSUUID != "uuid" || account.WireGuardPublicKey != "pub" || account.ShadowsocksUsername != "alice" {
		t.Fatalf("protocol material was lost: %#v", account)
	}
}
