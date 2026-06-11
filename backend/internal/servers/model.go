package servers

import (
	"time"

	"github.com/artuazh/routegate/backend/internal/agents"
)

const (
	StatusPending  = "pending"
	StatusActive   = "active"
	StatusOffline  = "offline"
	StatusDisabled = "disabled"
	StatusError    = "error"
)

type Server struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Location    string    `json:"location,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	PublicIP    string    `json:"publicIp,omitempty"`
	PrivateIP   string    `json:"privateIp,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// Hostname is retained for compatibility with the existing API while the
	// legacy servers.hostname column remains in the schema.
	Hostname string `json:"hostname,omitempty"`
}

type CreateServerInput struct {
	Name        string
	Description string
	Location    string
	Provider    string
	PublicIP    string
	PrivateIP   string
	Status      string
}

type UpdateServerInput struct {
	Name        *string
	Description *string
	Location    *string
	Provider    *string
	PublicIP    *string
	PrivateIP   *string
	Status      *string
}

type ServerFilter struct {
	Status   string
	Provider string
	Location string
	Search   string
	Limit    int
	Offset   int
}

type ServerWithAgent struct {
	Server Server        `json:"server"`
	Agent  *agents.Agent `json:"agent,omitempty"`
}
