package observability

import (
	"testing"
	"time"
)

func TestAgentTelemetrySnapshotValidate(t *testing.T) {
	logicalCPUs := 4
	memoryTotal := uint64(8 * 1024 * 1024 * 1024)
	memoryAvailable := uint64(4 * 1024 * 1024 * 1024)
	rootTotal := uint64(100 * 1024 * 1024 * 1024)
	rootFree := uint64(40 * 1024 * 1024 * 1024)

	snapshot := AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion,
		CollectedAt:   time.Now().UTC(),
		Host: AgentHostTelemetry{
			LogicalCPUs:          &logicalCPUs,
			MemoryTotalBytes:     &memoryTotal,
			MemoryAvailableBytes: &memoryAvailable,
			RootFSTotalBytes:     &rootTotal,
			RootFSFreeBytes:      &rootFree,
		},
		VPNCore: AgentVPNCoreTelemetry{
			Type:         "sing-box",
			Installed:    true,
			Version:      "sing-box version 1.12.0",
			ServiceState: "active",
		},
	}

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("validate telemetry: %v", err)
	}
}

func TestAgentTelemetrySnapshotRejectsUnsupportedSchema(t *testing.T) {
	snapshot := AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion + 1,
		CollectedAt:   time.Now().UTC(),
		VPNCore:       AgentVPNCoreTelemetry{Type: "sing-box", ServiceState: "unknown"},
	}
	if err := snapshot.Validate(); err == nil {
		t.Fatal("unsupported telemetry schema version must be rejected")
	}
}

func TestAgentTelemetrySnapshotRejectsImpossibleMemoryBounds(t *testing.T) {
	total := uint64(1024)
	available := uint64(2048)
	snapshot := AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion,
		CollectedAt:   time.Now().UTC(),
		Host:          AgentHostTelemetry{MemoryTotalBytes: &total, MemoryAvailableBytes: &available},
		VPNCore:       AgentVPNCoreTelemetry{Type: "sing-box", ServiceState: "unknown"},
	}
	if err := snapshot.Validate(); err == nil {
		t.Fatal("available memory above total must be rejected")
	}
}
