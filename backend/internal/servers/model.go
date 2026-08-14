package servers

import (
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

const (
	StatusPending  = "pending"
	StatusActive   = "active"
	StatusOffline  = "offline"
	StatusDisabled = "disabled"
	StatusError    = "error"
)

const (
	LocationSourceManual       = "manual"
	LocationSourceAutoDetected = "auto_detected"
)

const defaultVLESSPort = 443

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

	LocationCountry   string   `json:"locationCountry,omitempty"`
	LocationRegion    string   `json:"locationRegion,omitempty"`
	LocationCity      string   `json:"locationCity,omitempty"`
	LocationLatitude  *float64 `json:"locationLatitude,omitempty"`
	LocationLongitude *float64 `json:"locationLongitude,omitempty"`
	LocationSource    string   `json:"locationSource,omitempty"`

	// Hostname is retained for compatibility with the existing API while the
	// legacy servers.hostname column remains in the schema.
	Hostname string `json:"hostname,omitempty"`
}

type ProtocolSettings struct {
	ServerID          string    `json:"serverId"`
	VLESSPort         int       `json:"vlessPort"`
	VLESSFlow         string    `json:"vlessFlow,omitempty"`
	VLESSNetwork      string    `json:"vlessNetwork,omitempty"`
	RealityPublicKey  string    `json:"realityPublicKey,omitempty"`
	RealityShortID    string    `json:"realityShortId,omitempty"`
	RealityServerName string    `json:"realityServerName,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type RealityKeypair struct {
	PrivateKey string
	PublicKey  string
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

type UpdateServerGeographyInput struct {
	Country   string
	Region    string
	City      string
	Latitude  *float64
	Longitude *float64
	Source    string
}

type UpdateProtocolSettingsInput struct {
	VLESSPort         *int
	VLESSFlow         *string
	VLESSNetwork      *string
	RealityPublicKey  *string
	RealityShortID    *string
	RealityServerName *string
}

type UpdateRealityKeypairInput struct {
	PrivateKey string
	PublicKey  string
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
