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
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	DeploymentRole string    `json:"deploymentRole"`
	Description    string    `json:"description,omitempty"`
	Location       string    `json:"location,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	PublicIP       string    `json:"publicIp,omitempty"`
	PrivateIP      string    `json:"privateIp,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`

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
	Protocol          string    `json:"protocol"`
	VLESSPort         int       `json:"vlessPort"`
	VLESSFlow         string    `json:"vlessFlow,omitempty"`
	VLESSNetwork      string    `json:"vlessNetwork,omitempty"`
	RealityPublicKey  string    `json:"realityPublicKey,omitempty"`
	RealityShortID    string    `json:"realityShortId,omitempty"`
	RealityServerName string    `json:"realityServerName,omitempty"`
	WireGuardPort      int       `json:"wireGuardPort"`
	WireGuardAddress   string    `json:"wireGuardAddress"`
	WireGuardDNS       string    `json:"wireGuardDns"`
	WireGuardPublicKey string    `json:"wireGuardPublicKey,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type RealityKeypair struct {
	PrivateKey string
	PublicKey  string
}

type CreateServerInput struct {
	Name           string
	DeploymentRole string
	Description    string
	Location       string
	Provider       string
	PublicIP       string
	PrivateIP      string
	Status         string
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
	Protocol          *string
	VLESSPort         *int
	VLESSFlow         *string
	VLESSNetwork      *string
	RealityPublicKey  *string
	RealityShortID    *string
	RealityServerName *string
	WireGuardPort      *int
	WireGuardAddress   *string
	WireGuardDNS       *string
}

type UpdateRealityKeypairInput struct {
	PrivateKey string
	PublicKey  string
}

type UpdateWireGuardKeypairInput struct {
	PrivateKey string
	PublicKey  string
}

type ServerFilter struct {
	Status         string
	DeploymentRole string
	Provider       string
	Location       string
	Search         string
	Limit          int
	Offset         int
}

type ServerWithAgent struct {
	Server Server        `json:"server"`
	Agent  *agents.Agent `json:"agent,omitempty"`
}
