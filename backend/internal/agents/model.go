package agents

import "time"

const (
	StatusRegistered = "registered"
	StatusOnline     = "online"
	StatusOffline    = "offline"
	StatusDisabled   = "disabled"
	StatusError      = "error"
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
