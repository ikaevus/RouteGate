package platform

import "testing"

func TestManagedVPNCoreAdaptersDeclareVLESSWireGuardAndHysteria2(t *testing.T) {
	adapters := ManagedVPNCoreAdapters()
	if len(adapters) != 3 {
		t.Fatalf("adapter count = %d, want 3", len(adapters))
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
}
