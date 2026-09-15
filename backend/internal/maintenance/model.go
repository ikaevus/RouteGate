package maintenance

import "time"

const (
	ModeRecommended = "recommended"
	ModeAdvanced    = "advanced"

	StatusPlanned   = "planned"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusExpired   = "expired"
)

type Category struct {
	ID            string `json:"id"`
	Scope         string `json:"scope"`
	Recommended   bool   `json:"recommended"`
	Selectable    bool   `json:"selectable"`
	RetentionDays int    `json:"retentionDays"`
	BlockedReason string `json:"blockedReason,omitempty"`
}

type Analysis struct {
	Category
	CandidateCount int64 `json:"candidateCount"`
	EstimatedBytes int64 `json:"estimatedBytes,omitempty"`
}

type PlanItem struct {
	CategoryID     string         `json:"categoryId"`
	Scope          string         `json:"scope"`
	RetentionDays  int            `json:"retentionDays"`
	Cutoff         time.Time      `json:"cutoff"`
	CandidateCount int64          `json:"candidateCount"`
	EstimatedBytes int64          `json:"estimatedBytes,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type PlanPayload struct {
	SchemaVersion int        `json:"schemaVersion"`
	Items         []PlanItem `json:"items"`
}

type ProviderReport struct {
	CategoryID     string `json:"categoryId"`
	Status         string `json:"status"`
	DeletedCount   int64  `json:"deletedCount"`
	ReclaimedBytes int64  `json:"reclaimedBytes,omitempty"`
	RemainingCount int64  `json:"remainingCount"`
	ErrorCode      string `json:"errorCode,omitempty"`
}

type Report struct {
	SchemaVersion int              `json:"schemaVersion"`
	Status        string           `json:"status"`
	StartedAt     time.Time        `json:"startedAt"`
	CompletedAt   time.Time        `json:"completedAt"`
	Providers     []ProviderReport `json:"providers"`
}

type Plan struct {
	ID                 string      `json:"id"`
	Mode               string      `json:"mode"`
	Status             string      `json:"status"`
	SelectedCategories []string    `json:"selectedCategories"`
	Payload            PlanPayload `json:"payload"`
	CreatedByUserID    string      `json:"createdByUserId,omitempty"`
	CreatedAt          time.Time   `json:"createdAt"`
	ExpiresAt          time.Time   `json:"expiresAt"`
	StartedAt          *time.Time  `json:"startedAt,omitempty"`
	CompletedAt        *time.Time  `json:"completedAt,omitempty"`
	Report             *Report     `json:"report,omitempty"`
	ErrorCode          string      `json:"errorCode,omitempty"`
	confirmationHash   []byte
}

type CreatePlanRequest struct {
	Mode       string   `json:"mode"`
	Categories []string `json:"categories,omitempty"`
}

type CreatePlanResponse struct {
	Plan              Plan   `json:"plan"`
	ConfirmationToken string `json:"confirmationToken"`
}

type ExecutePlanRequest struct {
	ConfirmationToken string `json:"confirmationToken"`
}

type InventoryResponse struct {
	AnalyzedAt time.Time  `json:"analyzedAt"`
	Categories []Analysis `json:"categories"`
}
