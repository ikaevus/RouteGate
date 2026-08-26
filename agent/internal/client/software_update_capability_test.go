package client

import (
	"testing"

	"github.com/ikaevus/routegate/agent/internal/systeminfo"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestSoftwareUpdateCapabilityContract(t *testing.T) {
	for _, tc := range []struct {
		ready bool
		state string
	}{
		{ready: false, state: "contract_only"},
		{ready: true, state: "ready"},
	} {
		update := softwareUpdateCapability(tc.ready)
		if len(update) != 3 {
			t.Fatalf("softwareUpdate capability = %#v, want exactly three fields", update)
		}
		if got := update["schemaVersion"]; got != tasks.PlatformUpdateSchemaVersion {
			t.Fatalf("schemaVersion = %v, want %d", got, tasks.PlatformUpdateSchemaVersion)
		}
		if got := update["state"]; got != tc.state {
			t.Fatalf("state = %v, want %s", got, tc.state)
		}
		if got := update["request"]; got != "version_only" {
			t.Fatalf("request = %v, want version_only", got)
		}
	}
}

func TestAdvertisedCapabilitiesPreservesExistingCapabilities(t *testing.T) {
	capabilities := advertisedCapabilities(systeminfo.Info{Capabilities: map[string]any{"existing": true}})
	if _, ok := capabilities["softwareUpdate"].(map[string]any); !ok {
		t.Fatalf("softwareUpdate capability type = %T, want map[string]any", capabilities["softwareUpdate"])
	}
	if capabilities["existing"] != true {
		t.Fatal("existing capability was not preserved")
	}
}
