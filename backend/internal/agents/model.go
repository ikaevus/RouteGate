package agents

import (
	"encoding/json"
	"time"
)

const (
	StatusRegistered = "registered"
	StatusOnline     = "online"
	StatusOffline    = "offline"
	StatusDisabled   = "disabled"
	StatusError      = "error"
)

const (
	ConfigApplyJobStatusInProgress = "in_progress"
	ConfigApplyJobStatusSucceeded  = "succeeded"
	ConfigApplyJobStatusFailed     = "failed"
)

type Capabilities map[string]any

type Agent struct {
	ID           string       `json:"id"`
	ServerID     string       `json:"serverId"`
	Hostname     string       `json:"hostname,omitempty"`
	OS           string       `json:"os,omitempty"`
	Arch         string       `json:"arch,omitempty"`
	AgentVersion string       `json:"agentVersion"`
	Status       string       `json:"status"`
	TokenHash    string       `json:"-"`
	Capabilities Capabilities `json:"capabilities"`
	RegisteredAt time.Time    `json:"registeredAt"`
	LastSeenAt   *time.Time   `json:"lastSeenAt,omitempty"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`

	// These fields retain compatibility with the existing API response shape.
	Name     string    `json:"name,omitempty"`
	Version  string    `json:"version,omitempty"`
	LastSeen time.Time `json:"lastSeen,omitempty"`
}

type AgentConfigTask struct {
	ID              string          `json:"id"`
	ServerID        string          `json:"serverId"`
	AgentID         string          `json:"agentId"`
	ConfigVersionID string          `json:"configVersionId"`
	Action          string          `json:"action"`
	Status          string          `json:"status"`
	RenderedConfig  json.RawMessage `json:"renderedConfig"`
	ConfigHash      string          `json:"configHash"`
	CreatedAt       time.Time       `json:"createdAt"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
}

type CompleteConfigTaskInput struct {
	TokenHash     string
	JobID         string
	Status        string
	ErrorMessage  string
	ResultPayload map[string]any
}

type CreateOrReplaceAgentInput struct {
	ServerID     string
	Hostname     string
	OS           string
	Arch         string
	AgentVersion string
	TokenHash    string
	Capabilities Capabilities
	Status       string
	RegisteredAt *time.Time
	LastSeenAt   *time.Time
}

type UpdateAgentHeartbeatInput struct {
	AgentID      string
	TokenHash    string
	AgentVersion *string
	Capabilities Capabilities
}

type ServerRegistrationToken struct {
	ID        string     `json:"id"`
	ServerID  string     `json:"serverId"`
	TokenHash string     `json:"-"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type CreateRegistrationTokenInput struct {
	ServerID  string
	TokenHash string
	ExpiresAt *time.Time
}
