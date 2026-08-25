package client

import (
	"testing"

	"github.com/ikaevus/routegate/agent/internal/systeminfo"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestAdvertisedCapabilitiesMarksSoftwareUpdateContractOnly(t *testing.T) {
	capabilities := advertisedCapabilities(systeminfo.Info{Capabilities: map[string]any{"existing": true}})

	update, ok := capabilities["softwareUpdate"].(map[string]any)
	if !ok {
		t.Fatalf("softwareUpdate capability type = %T, want map[string]any", capabilities["softwareUpdate"])
	}
	if got := update["schemaVersion"]; got != tasks.PlatformUpdateSchemaVersion {
		t.Fatalf("schemaVersion = %v, want %d", got, tasks.PlatformUpdateSchemaVersion)
	}
	if got := update["state"]; got != "contract_only" {
		t.Fatalf("state = %v, want contract_only", got)
	}
	if got := update["request"]; got != "version_only" {
		t.Fatalf("request = %v, want version_only", got)
	}
	if capabilities["existing"] != true {
		t.Fatal("existing capability was not preserved")
	}
}
