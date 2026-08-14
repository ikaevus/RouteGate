package agents

import "time"

type HeartbeatTelemetry struct {
	SchemaVersion int                    `json:"schemaVersion"`
	CollectedAt   time.Time              `json:"collectedAt"`
	Host          HeartbeatHostTelemetry `json:"host"`
	VPNCore       HeartbeatVPNCore       `json:"vpnCore"`
}

type HeartbeatHostTelemetry struct {
	Load1               *float64 `json:"load1,omitempty"`
	Load5               *float64 `json:"load5,omitempty"`
	Load15              *float64 `json:"load15,omitempty"`
	LogicalCPUs          *int     `json:"logicalCpus,omitempty"`
	MemoryTotalBytes     *uint64  `json:"memoryTotalBytes,omitempty"`
	MemoryAvailableBytes *uint64  `json:"memoryAvailableBytes,omitempty"`
	RootFSTotalBytes     *uint64  `json:"rootFsTotalBytes,omitempty"`
	RootFSFreeBytes      *uint64  `json:"rootFsFreeBytes,omitempty"`
	UptimeSeconds        *uint64  `json:"uptimeSeconds,omitempty"`
}

type HeartbeatVPNCore struct {
	Type         string `json:"type"`
	Installed    bool   `json:"installed"`
	Version      string `json:"version,omitempty"`
	ServiceState string `json:"serviceState"`
}
