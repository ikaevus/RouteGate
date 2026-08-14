package analytics

import "time"

type Overview struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	Summary     OverviewSummary `json:"summary"`
	Nodes       []Node          `json:"nodes"`
	Alerts      []Alert         `json:"alerts"`
}

type OverviewSummary struct {
	TotalNodes     int `json:"totalNodes"`
	HealthyNodes   int `json:"healthyNodes"`
	DegradedNodes  int `json:"degradedNodes"`
	UnhealthyNodes int `json:"unhealthyNodes"`
	UnknownNodes   int `json:"unknownNodes"`
	ActiveAlerts   int `json:"activeAlerts"`
	CriticalAlerts int `json:"criticalAlerts"`
	LocatedNodes   int `json:"locatedNodes"`
}

type Node struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Status      string        `json:"status"`
	Provider    string        `json:"provider,omitempty"`
	PublicIP    string        `json:"publicIp,omitempty"`
	Location    NodeLocation  `json:"location"`
	Agent       NodeAgent     `json:"agent"`
	VPNCore     NodeVPNCore   `json:"vpnCore"`
	Resources   NodeResources `json:"resources"`
	Health      NodeHealth    `json:"health"`
	AlertCount  int           `json:"alertCount"`
	HasCritical bool          `json:"hasCriticalAlert"`
}

type NodeLocation struct {
	Label     string   `json:"label,omitempty"`
	Country   string   `json:"country,omitempty"`
	Region    string   `json:"region,omitempty"`
	City      string   `json:"city,omitempty"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	Source    string   `json:"source,omitempty"`
}

type NodeAgent struct {
	Status                string     `json:"status,omitempty"`
	Version               string     `json:"version,omitempty"`
	LastSeenAt            *time.Time `json:"lastSeenAt,omitempty"`
	ObservationReceivedAt *time.Time `json:"observationReceivedAt,omitempty"`
	ObservationAgeSeconds *float64   `json:"observationAgeSeconds,omitempty"`
	ObservationFresh      bool       `json:"observationFresh"`
}

type NodeVPNCore struct {
	Type         string `json:"type,omitempty"`
	Installed    bool   `json:"installed"`
	Version      string `json:"version,omitempty"`
	ServiceState string `json:"serviceState,omitempty"`
}

type NodeResources struct {
	Load1                *float64 `json:"load1,omitempty"`
	LogicalCPUs          *int64   `json:"logicalCpus,omitempty"`
	MemoryUsageRatio     *float64 `json:"memoryUsageRatio,omitempty"`
	RootFSUsageRatio     *float64 `json:"rootFsUsageRatio,omitempty"`
	HostUptimeSeconds    *int64   `json:"hostUptimeSeconds,omitempty"`
}

type NodeHealth struct {
	State             string `json:"state"`
	ReasonCode        string `json:"reasonCode,omitempty"`
	Summary           string `json:"summary,omitempty"`
	RecommendedAction string `json:"recommendedAction,omitempty"`
}

type Alert struct {
	ID           string     `json:"id"`
	ServerID     string     `json:"serverId"`
	ServerName   string     `json:"serverName"`
	Severity     string     `json:"severity"`
	State        string     `json:"state"`
	Summary      string     `json:"summary"`
	ReasonCode   string     `json:"reasonCode,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	FiringAt     *time.Time `json:"firingAt,omitempty"`
	Acknowledged bool       `json:"acknowledged"`
}
