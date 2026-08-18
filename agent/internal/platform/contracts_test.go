package platform

import "testing"

func TestManagedVPNCoreAdaptersDeclareOnlySingBoxVLESS(t *testing.T) {
	adapters := ManagedVPNCoreAdapters()
	if len(adapters) != 1 {
		t.Fatalf("adapter count = %d, want 1", len(adapters))
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
}
