package traffic

import "time"

const (
	DefaultResetDay      = 1
	MaxUsageReportEvents = 1000

	TrafficLimitEnforcementNotEnforced = "not_enforced"
	TrafficLimitEnforcementWithinLimit = "within_limit"
	TrafficLimitEnforcementOverLimit   = "over_limit"
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
	VPNAccountID         string     `json:"vpnAccountId"`
	MonthlyLimitBytes    *int64     `json:"monthlyLimitBytes,omitempty"`
	HardLimitEnabled     bool       `json:"hardLimitEnabled"`
	SpeedLimitBps        *int64     `json:"speedLimitBps,omitempty"`
	ResetDay             int        `json:"resetDay"`
	LimitExceededAt      *time.Time `json:"limitExceededAt,omitempty"`
	EnforcementStatus    string     `json:"enforcementStatus"`
	EnforcementUpdatedAt *time.Time `json:"enforcementUpdatedAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
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
	MonthlyLimitBytes    *int64     `json:"monthlyLimitBytes,omitempty"`
	HardLimitEnabled     bool       `json:"hardLimitEnabled"`
	SpeedLimitBps        *int64     `json:"speedLimitBps,omitempty"`
	ResetDay             int        `json:"resetDay"`
	UsedPercent          *float64   `json:"usedPercent,omitempty"`
	RemainingBytes       *int64     `json:"remainingBytes,omitempty"`
	LimitReached         bool       `json:"limitReached"`
	Enforced             bool       `json:"enforced"`
	LimitExceededAt      *time.Time `json:"limitExceededAt,omitempty"`
	EnforcementStatus    string     `json:"enforcementStatus"`
	EnforcementUpdatedAt *time.Time `json:"enforcementUpdatedAt,omitempty"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type TrafficUsageSummary struct {
	VPNAccountID string             `json:"vpnAccountId"`
	Period       TrafficPeriod      `json:"period"`
	Usage        TrafficUsageTotals `json:"usage"`
	Limit        *TrafficLimitState `json:"limit,omitempty"`
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

type trafficLimitEnforcementEvaluation struct {
	Status     string
	ExceededAt *time.Time
	Enforced   bool
}

func evaluateTrafficLimitEnforcement(limit TrafficLimit, totalBytes int64, evaluatedAt time.Time) trafficLimitEnforcementEvaluation {
	if limit.MonthlyLimitBytes == nil || *limit.MonthlyLimitBytes <= 0 || !limit.HardLimitEnabled {
		return trafficLimitEnforcementEvaluation{Status: TrafficLimitEnforcementNotEnforced}
	}

	if totalBytes < *limit.MonthlyLimitBytes {
		return trafficLimitEnforcementEvaluation{Status: TrafficLimitEnforcementWithinLimit}
	}

	exceededAt := limit.LimitExceededAt
	if exceededAt == nil && !evaluatedAt.IsZero() {
		value := evaluatedAt.UTC()
		exceededAt = &value
	}

	return trafficLimitEnforcementEvaluation{
		Status:     TrafficLimitEnforcementOverLimit,
		ExceededAt: exceededAt,
		Enforced:   true,
	}
}
