package platform

import "testing"

func TestManagedVPNCoreAdaptersDeclareVLESSAndWireGuard(t *testing.T) {
	adapters := ManagedVPNCoreAdapters()
	if len(adapters) != 2 {
		t.Fatalf("adapter count = %d, want 2", len(adapters))
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
}
