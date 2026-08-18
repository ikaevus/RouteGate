package configs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

func TestDefaultVPNCoreAdapterRegistryExposesOnlyManagedVLESSPath(t *testing.T) {
	registry := defaultVPNCoreAdapterRegistry()
	if len(registry.adapters) != 1 {
		t.Fatalf("adapter count = %d, want 1", len(registry.adapters))
	}
	descriptor := registry.adapters[0].Descriptor()
	if descriptor.Core != platform.VPNCoreSingBox || descriptor.Protocol != platform.VPNProtocolVLESS {
		t.Fatalf("unexpected default adapter: %+v", descriptor)
	}
	if len(descriptor.Transports) != 1 || descriptor.Transports[0] != platform.VPNTransportTCP {
		t.Fatalf("unexpected transports: %+v", descriptor.Transports)
	}
	if len(descriptor.SecurityModes) != 2 ||
		descriptor.SecurityModes[0] != platform.VPNSecurityNone ||
		descriptor.SecurityModes[1] != platform.VPNSecurityReality {
		t.Fatalf("unexpected security modes: %+v", descriptor.SecurityModes)
	}
}

func TestVPNCoreAdapterBoundaryPreservesRouteGateConfigV1Envelope(t *testing.T) {
	config := buildRenderedConfig(ServerConfigInfo{
		ID:     "server-id",
		Name:   "fi-01",
		Status: "active",
	}, time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC))

	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal rendered config: %v", err)
	}
	want := `{"schemaVersion":"routegate.config.v1","server":{"id":"server-id","name":"fi-01","deploymentRole":"vpn","status":"active"},"vpnAccounts":[],"singBox":{"log":{"level":"info"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"rules":[],"final":"direct"}},"metadata":{"source":"routegate-manager","renderedAt":"2026-08-18T12:00:00Z","realityEnabled":false}}`
	if string(payload) != want {
		t.Fatalf("routegate.config.v1 changed:\n got: %s\nwant: %s", payload, want)
	}
}

func TestDefaultVPNCoreAdapterRegistryRejectsUnsupportedComposition(t *testing.T) {
	registry := defaultVPNCoreAdapterRegistry()
	if _, ok := registry.Resolve(platform.VPNCoreSingBox, platform.VPNProtocolVLESS, "quic", platform.VPNSecurityReality); ok {
		t.Fatal("registry must reject an undeclared transport")
	}
	if _, ok := registry.Resolve(platform.VPNCoreSingBox, "wireguard", platform.VPNTransportTCP, platform.VPNSecurityNone); ok {
		t.Fatal("registry must reject an unregistered protocol")
	}
}
