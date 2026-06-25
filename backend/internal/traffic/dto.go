package traffic

import "time"

type ReportUsageRequest struct {
	Events []ReportUsageEventRequest `json:"events"`
}

type ReportUsageEventRequest struct {
	VPNAccountID string         `json:"vpnAccountId"`
	RxBytes      int64          `json:"rxBytes"`
	TxBytes      int64          `json:"txBytes"`
	ObservedAt   *time.Time     `json:"observedAt"`
	Metadata     map[string]any `json:"metadata"`
}

type UpdateTrafficLimitRequest struct {
	MonthlyLimitBytes *int64 `json:"monthlyLimitBytes"`
	HardLimitEnabled  bool   `json:"hardLimitEnabled"`
	SpeedLimitBps     *int64 `json:"speedLimitBps"`
	ResetDay          *int   `json:"resetDay"`
}
