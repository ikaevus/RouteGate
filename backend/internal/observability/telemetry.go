package observability

import (
	"fmt"
	"strings"
	"time"
)

const AgentTelemetrySchemaVersion = 1

// AgentTelemetrySnapshot is the bounded, latest-only operational observation
// sent by RouteGate Agent. It is deliberately a fixed-cardinality contract;
// arbitrary labels or unbounded metric maps do not cross this boundary.
type AgentTelemetrySnapshot struct {
	SchemaVersion int                    `json:"schemaVersion"`
	CollectedAt   time.Time              `json:"collectedAt"`
	Host          AgentHostTelemetry     `json:"host"`
	VPNCore       AgentVPNCoreTelemetry  `json:"vpnCore"`
}

type AgentHostTelemetry struct {
	Load1                *float64 `json:"load1,omitempty"`
	Load5                *float64 `json:"load5,omitempty"`
	Load15               *float64 `json:"load15,omitempty"`
	LogicalCPUs           *int     `json:"logicalCpus,omitempty"`
	MemoryTotalBytes      *uint64  `json:"memoryTotalBytes,omitempty"`
	MemoryAvailableBytes  *uint64  `json:"memoryAvailableBytes,omitempty"`
	RootFSTotalBytes      *uint64  `json:"rootFsTotalBytes,omitempty"`
	RootFSFreeBytes       *uint64  `json:"rootFsFreeBytes,omitempty"`
	UptimeSeconds         *uint64  `json:"uptimeSeconds,omitempty"`
}

type AgentVPNCoreTelemetry struct {
	Type         string `json:"type"`
	Installed    bool   `json:"installed"`
	Version      string `json:"version,omitempty"`
	ServiceState string `json:"serviceState"`
}

func (s AgentTelemetrySnapshot) Validate() error {
	if s.SchemaVersion != AgentTelemetrySchemaVersion {
		return fmt.Errorf("unsupported telemetry schema version %d", s.SchemaVersion)
	}
	if s.CollectedAt.IsZero() {
		return fmt.Errorf("telemetry collectedAt is required")
	}
	if strings.TrimSpace(s.VPNCore.Type) == "" {
		return fmt.Errorf("telemetry vpnCore.type is required")
	}
	if strings.TrimSpace(s.VPNCore.ServiceState) == "" {
		return fmt.Errorf("telemetry vpnCore.serviceState is required")
	}
	if s.VPNCore.Version != "" && strings.TrimSpace(s.VPNCore.Version) == "" {
		return fmt.Errorf("telemetry vpnCore.version must not be blank")
	}
	if err := validateNonNegativeFloat("host.load1", s.Host.Load1); err != nil {
		return err
	}
	if err := validateNonNegativeFloat("host.load5", s.Host.Load5); err != nil {
		return err
	}
	if err := validateNonNegativeFloat("host.load15", s.Host.Load15); err != nil {
		return err
	}
	if s.Host.LogicalCPUs != nil && *s.Host.LogicalCPUs <= 0 {
		return fmt.Errorf("telemetry host.logicalCpus must be positive")
	}
	if s.Host.MemoryTotalBytes != nil && *s.Host.MemoryTotalBytes == 0 {
		return fmt.Errorf("telemetry host.memoryTotalBytes must be positive")
	}
	if s.Host.MemoryTotalBytes != nil && s.Host.MemoryAvailableBytes != nil && *s.Host.MemoryAvailableBytes > *s.Host.MemoryTotalBytes {
		return fmt.Errorf("telemetry host.memoryAvailableBytes exceeds total memory")
	}
	if s.Host.RootFSTotalBytes != nil && *s.Host.RootFSTotalBytes == 0 {
		return fmt.Errorf("telemetry host.rootFsTotalBytes must be positive")
	}
	if s.Host.RootFSTotalBytes != nil && s.Host.RootFSFreeBytes != nil && *s.Host.RootFSFreeBytes > *s.Host.RootFSTotalBytes {
		return fmt.Errorf("telemetry host.rootFsFreeBytes exceeds total filesystem size")
	}
	return nil
}

func validateNonNegativeFloat(name string, value *float64) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("telemetry %s must not be negative", name)
	}
	return nil
}
