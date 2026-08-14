package observability

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

const AgentTelemetryHealthTTL = 90 * time.Second

const (
	CheckAgentTelemetryFreshness = "agent.telemetry.freshness"
	CheckHostMemoryCapacity      = "host.memory.capacity"
	CheckHostDiskCapacity        = "host.disk.capacity"
	CheckVPNCoreService          = "vpn_core.service"
)

// EvaluateAgentTelemetry converts a bounded telemetry observation into stable
// operational health checks. Manager receive time owns freshness semantics so
// Agent clock skew cannot make evidence appear fresh forever or violate expiry
// ordering. The Agent collectedAt timestamp remains in the telemetry record.
func EvaluateAgentTelemetry(resource ResourceRef, snapshot AgentTelemetrySnapshot, receivedAt time.Time) []HealthCheck {
	receivedAt = receivedAt.UTC()
	expiresAt := receivedAt.Add(AgentTelemetryHealthTTL)
	observedAt := receivedAt

	return []HealthCheck{
		{
			Key:               CheckAgentTelemetryFreshness,
			Resource:          resource,
			Component:         "agent",
			State:             HealthHealthy,
			Required:          true,
			ReasonCode:        "telemetry_recent",
			Summary:           "Agent telemetry is current.",
			RecommendedAction: "",
			Evidence:          healthEvidence(map[string]any{"receivedAt": receivedAt, "collectedAt": snapshot.CollectedAt.UTC(), "ttlSeconds": int(AgentTelemetryHealthTTL.Seconds())}),
			ObservedAt:        observedAt,
			ExpiresAt:         &expiresAt,
		},
		evaluateMemoryHealth(resource, snapshot.Host, observedAt, expiresAt),
		evaluateDiskHealth(resource, snapshot.Host, observedAt, expiresAt),
		evaluateVPNCoreHealth(resource, snapshot.VPNCore, observedAt, expiresAt),
	}
}

func evaluateMemoryHealth(resource ResourceRef, host AgentHostTelemetry, observedAt, expiresAt time.Time) HealthCheck {
	check := HealthCheck{
		Key:        CheckHostMemoryCapacity,
		Resource:   resource,
		Component:  "host",
		Required:   true,
		ObservedAt: observedAt,
		ExpiresAt:  &expiresAt,
	}
	if host.MemoryTotalBytes == nil || host.MemoryAvailableBytes == nil || *host.MemoryTotalBytes == 0 {
		check.State = HealthUnknown
		check.ReasonCode = "memory_capacity_unavailable"
		check.Summary = "Memory capacity could not be evaluated."
		check.RecommendedAction = "check_agent_telemetry"
		return check
	}

	availablePercent := percent(*host.MemoryAvailableBytes, *host.MemoryTotalBytes)
	check.Evidence = healthEvidence(map[string]any{
		"totalBytes":     *host.MemoryTotalBytes,
		"availableBytes": *host.MemoryAvailableBytes,
		"availablePct":   availablePercent,
	})
	switch {
	case availablePercent <= 5:
		check.State = HealthUnhealthy
		check.ReasonCode = "memory_available_critical"
		check.Summary = "Available memory is critically low."
		check.RecommendedAction = "investigate_memory_pressure"
	case availablePercent <= 15:
		check.State = HealthDegraded
		check.ReasonCode = "memory_available_low"
		check.Summary = "Available memory is low."
		check.RecommendedAction = "investigate_memory_pressure"
	default:
		check.State = HealthHealthy
		check.ReasonCode = "memory_capacity_ok"
		check.Summary = "Available memory is within the healthy range."
	}
	return check
}

func evaluateDiskHealth(resource ResourceRef, host AgentHostTelemetry, observedAt, expiresAt time.Time) HealthCheck {
	check := HealthCheck{
		Key:        CheckHostDiskCapacity,
		Resource:   resource,
		Component:  "host",
		Required:   true,
		ObservedAt: observedAt,
		ExpiresAt:  &expiresAt,
	}
	if host.RootFSTotalBytes == nil || host.RootFSFreeBytes == nil || *host.RootFSTotalBytes == 0 {
		check.State = HealthUnknown
		check.ReasonCode = "disk_capacity_unavailable"
		check.Summary = "Root filesystem capacity could not be evaluated."
		check.RecommendedAction = "check_agent_telemetry"
		return check
	}

	freePercent := percent(*host.RootFSFreeBytes, *host.RootFSTotalBytes)
	check.Evidence = healthEvidence(map[string]any{
		"totalBytes": *host.RootFSTotalBytes,
		"freeBytes":  *host.RootFSFreeBytes,
		"freePct":    freePercent,
	})
	switch {
	case freePercent <= 5:
		check.State = HealthUnhealthy
		check.ReasonCode = "disk_free_critical"
		check.Summary = "Root filesystem free space is critically low."
		check.RecommendedAction = "free_disk_space"
	case freePercent <= 15:
		check.State = HealthDegraded
		check.ReasonCode = "disk_free_low"
		check.Summary = "Root filesystem free space is low."
		check.RecommendedAction = "free_disk_space"
	default:
		check.State = HealthHealthy
		check.ReasonCode = "disk_capacity_ok"
		check.Summary = "Root filesystem capacity is within the healthy range."
	}
	return check
}

func evaluateVPNCoreHealth(resource ResourceRef, core AgentVPNCoreTelemetry, observedAt, expiresAt time.Time) HealthCheck {
	check := HealthCheck{
		Key:        CheckVPNCoreService,
		Resource:   resource,
		Component:  "vpn_core",
		Required:   true,
		ObservedAt: observedAt,
		ExpiresAt:  &expiresAt,
		Evidence: healthEvidence(map[string]any{
			"type":         strings.TrimSpace(core.Type),
			"installed":    core.Installed,
			"version":      strings.TrimSpace(core.Version),
			"serviceState": strings.TrimSpace(core.ServiceState),
		}),
	}

	state := strings.ToLower(strings.TrimSpace(core.ServiceState))
	if !core.Installed || state == "not_installed" {
		check.State = HealthUnhealthy
		check.ReasonCode = "vpn_core_not_installed"
		check.Summary = "VPN Core is not installed."
		check.RecommendedAction = "install_vpn_core"
		return check
	}

	switch state {
	case "active":
		check.State = HealthHealthy
		check.ReasonCode = "vpn_core_running"
		check.Summary = "VPN Core service is running."
	case "activating", "reloading":
		check.State = HealthDegraded
		check.ReasonCode = "vpn_core_transitioning"
		check.Summary = "VPN Core service is transitioning."
		check.RecommendedAction = "wait_for_vpn_core"
	case "inactive", "deactivating", "failed":
		check.State = HealthUnhealthy
		check.ReasonCode = "vpn_core_not_running"
		check.Summary = "VPN Core service is not running."
		check.RecommendedAction = "start_vpn_core"
	default:
		check.State = HealthUnknown
		check.ReasonCode = "vpn_core_state_unknown"
		check.Summary = "VPN Core service state is unknown."
		check.RecommendedAction = "check_vpn_core_service"
	}
	return check
}

func percent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return math.Round((float64(part)/float64(total))*10000) / 100
}

func healthEvidence(value map[string]any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return payload
}
