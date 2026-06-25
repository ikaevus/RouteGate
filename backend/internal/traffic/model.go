package traffic

import "time"

const (
	DefaultResetDay      = 1
	MaxUsageReportEvents = 1000
)

type TrafficUsageEvent struct {
	ID           string         `json:"id"`
	ServerID     string         `json:"serverId"`
	AgentID      string         `json:"agentId,omitempty"`
	VPNAccountID string         `json:"vpnAccountId"`
	RxBytes      int64          `json:"rxBytes"`
	TxBytes      int64          `json:"txBytes"`
	TotalBytes   int64          `json:"totalBytes"`
	ObservedAt   time.Time      `json:"observedAt"`
	ReportedAt   time.Time      `json:"reportedAt"`
	Source       string         `json:"source"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type TrafficLimit struct {
	VPNAccountID       string    `json:"vpnAccountId"`
	MonthlyLimitBytes  *int64    `json:"monthlyLimitBytes,omitempty"`
	HardLimitEnabled   bool      `json:"hardLimitEnabled"`
	SpeedLimitBps      *int64    `json:"speedLimitBps,omitempty"`
	ResetDay           int       `json:"resetDay"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type TrafficPeriod struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type TrafficUsageTotals struct {
	RxBytes    int64 `json:"rxBytes"`
	TxBytes    int64 `json:"txBytes"`
	TotalBytes int64 `json:"totalBytes"`
}

type TrafficLimitState struct {
	MonthlyLimitBytes *int64   `json:"monthlyLimitBytes,omitempty"`
	HardLimitEnabled  bool     `json:"hardLimitEnabled"`
	SpeedLimitBps     *int64   `json:"speedLimitBps,omitempty"`
	ResetDay          int      `json:"resetDay"`
	UsedPercent       *float64 `json:"usedPercent,omitempty"`
	LimitReached      bool     `json:"limitReached"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type TrafficUsageSummary struct {
	VPNAccountID string              `json:"vpnAccountId"`
	Period       TrafficPeriod       `json:"period"`
	Usage        TrafficUsageTotals  `json:"usage"`
	Limit        *TrafficLimitState  `json:"limit,omitempty"`
}

type CreateUsageEventInput struct {
	VPNAccountID string
	RxBytes      int64
	TxBytes      int64
	ObservedAt   time.Time
	Metadata     map[string]any
}

type UpsertTrafficLimitInput struct {
	MonthlyLimitBytes *int64
	HardLimitEnabled  bool
	SpeedLimitBps     *int64
	ResetDay          int
}

type TrafficUsageReport struct {
	OK       bool   `json:"ok"`
	AgentID  string `json:"agentId"`
	ServerID string `json:"serverId"`
	Accepted int    `json:"accepted"`
}
