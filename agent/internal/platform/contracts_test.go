package platform

import "testing"

func TestManagedVPNCoreAdaptersDeclareManagedCompositions(t *testing.T) {
	adapters := ManagedVPNCoreAdapters()
	if len(adapters) != 5 {
		t.Fatalf("adapter count = %d, want 5", len(adapters))
	}
	descriptor := adapters[0]
	if descriptor.Core != VPNCoreSingBox || descriptor.Protocol != VPNProtocolVLESS {
		t.Fatalf("unexpected adapter: %+v", descriptor)
	}
	if len(descriptor.Transports) != 1 || descriptor.Transports[0] != VPNTransportTCP {
		t.Fatalf("unexpected transports: %+v", descriptor.Transports)
	}
	if len(descriptor.SecurityModes) != 2 ||
		descriptor.SecurityModes[0] != VPNSecurityNone ||
		descriptor.SecurityModes[1] != VPNSecurityReality {
		t.Fatalf("unexpected security modes: %+v", descriptor.SecurityModes)
	}
	wireGuard := adapters[1]
	if wireGuard.Core != VPNCoreWireGuard || wireGuard.Protocol != VPNProtocolWireGuard || wireGuard.Transports[0] != VPNTransportUDP || wireGuard.SecurityModes[0] != VPNSecurityWireGuard {
		t.Fatalf("unexpected WireGuard adapter: %+v", wireGuard)
	}
	hysteria2 := adapters[2]
	if hysteria2.Core != VPNCoreHysteria || hysteria2.Protocol != VPNProtocolHysteria2 || hysteria2.Transports[0] != VPNTransportQUIC || hysteria2.SecurityModes[0] != VPNSecurityTLS {
		t.Fatalf("unexpected Hysteria2 adapter: %+v", hysteria2)
	}
	shadowsocks := adapters[3]
	if shadowsocks.Core != VPNCoreSingBox || shadowsocks.Protocol != VPNProtocolShadowsocks || shadowsocks.Transports[0] != VPNTransportTCP || shadowsocks.SecurityModes[0] != VPNSecurityAEAD2022 {
		t.Fatalf("unexpected Shadowsocks adapter: %+v", shadowsocks)
	}
	mtproto := adapters[4]
	if mtproto.Core != VPNCoreMTG || mtproto.Protocol != VPNProtocolMTProto || mtproto.Transports[0] != VPNTransportTCP || mtproto.SecurityModes[0] != VPNSecurityFakeTLS {
		t.Fatalf("unexpected MTProto adapter: %+v", mtproto)
	}
}
