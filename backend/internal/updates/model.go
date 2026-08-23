package updates

import (
	"encoding/json"
	"time"
)

const (
	OperationPreflight = "preflight"
	StagePreflight     = "preflight"

	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"

	DecisionProceed = "proceed"
	DecisionBlocked = "blocked"

	HostTrustPreflightDeferred = "deferred_to_privileged_b2"
)

type Job struct {
	ID              string          `json:"id"`
	Operation       string          `json:"operation"`
	Status          string          `json:"status"`
	Stage           string          `json:"stage"`
	RequestPayload  json.RawMessage `json:"requestPayload"`
	ResultPayload   json.RawMessage `json:"resultPayload"`
	ErrorCode       string          `json:"errorCode,omitempty"`
	CreatedByUserID string          `json:"createdByUserId,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
}

type PreflightResult struct {
	Decision                  string   `json:"decision"`
	Blockers                  []string `json:"blockers"`
	ManagerVersion            string   `json:"managerVersion"`
	ManagerGitCommit          string   `json:"managerGitCommit"`
	ManagerBuildDate          string   `json:"managerBuildDate"`
	DatabaseAppliedMigration  string   `json:"databaseAppliedMigration"`
	ExpectedSchemaVersion     int      `json:"expectedSchemaVersion"`
	UpdateStatus              string   `json:"updateStatus"`
	UpdateChannel             string   `json:"updateChannel"`
	AutomaticUpdatesSupported bool     `json:"automaticUpdatesSupported"`
	HostTrustPreflight        string   `json:"hostTrustPreflight"`
}

type ListResponse struct {
	Items []Job `json:"items"`
}

type CreateResponse struct {
	Job Job `json:"job"`
}
