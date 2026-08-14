package observability

import (
	"testing"
	"time"
)

func TestEvaluateAgentTelemetryHealthy(t *testing.T) {
	memoryTotal := uint64(8 * 1024 * 1024 * 1024)
	memoryAvailable := uint64(4 * 1024 * 1024 * 1024)
	diskTotal := uint64(100 * 1024 * 1024 * 1024)
	diskFree := uint64(40 * 1024 * 1024 * 1024)
	collectedAt := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	receivedAt := collectedAt.Add(2 * time.Second)

	checks := EvaluateAgentTelemetry(ResourceRef{Type: "server", ID: "server-1"}, AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion,
		CollectedAt:   collectedAt,
		Host: AgentHostTelemetry{
			MemoryTotalBytes:     &memoryTotal,
			MemoryAvailableBytes: &memoryAvailable,
			RootFSTotalBytes:     &diskTotal,
			RootFSFreeBytes:      &diskFree,
		},
		VPNCore: AgentVPNCoreTelemetry{Type: "sing-box", Installed: true, ServiceState: "active"},
	}, receivedAt)

	if len(checks) != 4 {
		t.Fatalf("checks = %d, want 4", len(checks))
	}
	for _, check := range checks {
		if check.State != HealthHealthy {
			t.Fatalf("check %q state = %q, want healthy", check.Key, check.State)
		}
		if check.ExpiresAt == nil || !check.ExpiresAt.Equal(receivedAt.Add(AgentTelemetryHealthTTL)) {
			t.Fatalf("check %q expiry = %v", check.Key, check.ExpiresAt)
		}
	}
}

func TestEvaluateAgentTelemetryCapacityThresholds(t *testing.T) {
	memoryTotal := uint64(1000)
	memoryAvailable := uint64(50)
	diskTotal := uint64(1000)
	diskFree := uint64(150)
	now := time.Now().UTC()

	checks := EvaluateAgentTelemetry(ResourceRef{Type: "server", ID: "server-1"}, AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion,
		CollectedAt:   now,
		Host: AgentHostTelemetry{
			MemoryTotalBytes:     &memoryTotal,
			MemoryAvailableBytes: &memoryAvailable,
			RootFSTotalBytes:     &diskTotal,
			RootFSFreeBytes:      &diskFree,
		},
		VPNCore: AgentVPNCoreTelemetry{Type: "sing-box", Installed: true, ServiceState: "active"},
	}, now)

	memory := checkByKey(t, checks, CheckHostMemoryCapacity)
	if memory.State != HealthUnhealthy || memory.ReasonCode != "memory_available_critical" {
		t.Fatalf("unexpected memory health: %+v", memory)
	}
	disk := checkByKey(t, checks, CheckHostDiskCapacity)
	if disk.State != HealthDegraded || disk.ReasonCode != "disk_free_low" {
		t.Fatalf("unexpected disk health: %+v", disk)
	}
}

func TestEvaluateAgentTelemetryMissingEvidenceIsUnknown(t *testing.T) {
	now := time.Now().UTC()
	checks := EvaluateAgentTelemetry(ResourceRef{Type: "server", ID: "server-1"}, AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion,
		CollectedAt:   now,
		VPNCore:       AgentVPNCoreTelemetry{Type: "sing-box", Installed: true, ServiceState: "unknown"},
	}, now)

	if checkByKey(t, checks, CheckHostMemoryCapacity).State != HealthUnknown {
		t.Fatal("missing memory evidence must evaluate to unknown")
	}
	if checkByKey(t, checks, CheckHostDiskCapacity).State != HealthUnknown {
		t.Fatal("missing disk evidence must evaluate to unknown")
	}
	if checkByKey(t, checks, CheckVPNCoreService).State != HealthUnknown {
		t.Fatal("unknown VPN Core service state must evaluate to unknown")
	}
}

func TestEvaluatedHealthExpiresToUnknown(t *testing.T) {
	now := time.Now().UTC()
	checks := EvaluateAgentTelemetry(ResourceRef{Type: "server", ID: "server-1"}, AgentTelemetrySnapshot{
		SchemaVersion: AgentTelemetrySchemaVersion,
		CollectedAt:   now,
		VPNCore:       AgentVPNCoreTelemetry{Type: "sing-box", Installed: true, ServiceState: "active"},
	}, now)

	freshness := checkByKey(t, checks, CheckAgentTelemetryFreshness)
	if freshness.EffectiveState(now.Add(AgentTelemetryHealthTTL-time.Second)) != HealthHealthy {
		t.Fatal("fresh health evidence expired too early")
	}
	if freshness.EffectiveState(now.Add(AgentTelemetryHealthTTL)) != HealthUnknown {
		t.Fatal("expired health evidence must become unknown")
	}
}

func checkByKey(t *testing.T, checks []HealthCheck, key string) HealthCheck {
	t.Helper()
	for _, check := range checks {
		if check.Key == key {
			return check
		}
	}
	t.Fatalf("check %q not found", key)
	return HealthCheck{}
}
