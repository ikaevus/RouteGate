package servers

import (
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

type CreateServerRequest struct {
	Name           string `json:"name"`
	DeploymentRole string `json:"deploymentRole,omitempty"`
	Description    string `json:"description"`
	Location       string `json:"location"`
	Provider       string `json:"provider"`
	PublicIP       string `json:"publicIp"`
	PrivateIP      string `json:"privateIp"`

	// Hostname is accepted for backwards compatibility with the legacy admin API.
	Hostname string `json:"hostname,omitempty"`
}

type UpdateServerRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Location    *string `json:"location"`
	Provider    *string `json:"provider"`
	PublicIP    *string `json:"publicIp"`
	PrivateIP   *string `json:"privateIp"`
	Status      *string `json:"status"`
}

type UpdateServerGeographyRequest struct {
	Country   string   `json:"country"`
	Region    string   `json:"region"`
	City      string   `json:"city"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Source    string   `json:"source"`
}

type UpdateProtocolSettingsRequest struct {
	Protocol          *string `json:"protocol"`
	VLESSPort         *int    `json:"vlessPort"`
	VLESSFlow         *string `json:"vlessFlow"`
	VLESSNetwork      *string `json:"vlessNetwork"`
	RealityPublicKey  *string `json:"realityPublicKey"`
	RealityShortID    *string `json:"realityShortId"`
	RealityServerName *string `json:"realityServerName"`
	WireGuardPort      *int    `json:"wireGuardPort"`
	WireGuardAddress   *string `json:"wireGuardAddress"`
	WireGuardDNS       *string `json:"wireGuardDns"`
}

type ServerResponse struct {
	Server
	Agent     *agents.Agent         `json:"agent,omitempty"`
	Inventory NodeInventorySummary `json:"inventory"`
}

type ListServersResponse struct {
	Items []ServerResponse `json:"items"`
}

type LegacyListServersResponse struct {
	Items []Server `json:"items"`
}

type RegistrationTokenResponse struct {
	ServerID          string    `json:"serverId"`
	RegistrationToken string    `json:"registrationToken"`
	ExpiresAt         time.Time `json:"expiresAt"`
	ManagerURL        string    `json:"managerUrl,omitempty"`
	BootstrapCommand  string    `json:"bootstrapCommand,omitempty"`
}

type ProtocolSettingsResponse struct {
	ServerID string `json:"serverId"`
	Protocol string `json:"protocol"`
	VLESS    struct {
		Port    int    `json:"port"`
		Flow    string `json:"flow,omitempty"`
		Network string `json:"network,omitempty"`
	} `json:"vless"`
	Reality struct {
		Enabled    bool   `json:"enabled"`
		PublicKey  string `json:"publicKey,omitempty"`
		ShortID    string `json:"shortId,omitempty"`
		ServerName string `json:"serverName,omitempty"`
	} `json:"reality"`
	WireGuard struct {
		Port      int    `json:"port"`
		Address   string `json:"address"`
		DNS       string `json:"dns"`
		PublicKey string `json:"publicKey,omitempty"`
		Ready     bool   `json:"ready"`
	} `json:"wireGuard"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func newServerResponse(server ServerWithAgent, now time.Time) ServerResponse {
	return ServerResponse{
		Server:    server.Server,
		Agent:     server.Agent,
		Inventory: newNodeInventorySummary(server, now),
	}
}

func newProtocolSettingsResponse(settings ProtocolSettings) ProtocolSettingsResponse {
	protocol := settings.Protocol
	if protocol == "" {
		protocol = "vless"
	}
	response := ProtocolSettingsResponse{
		ServerID:  settings.ServerID,
		Protocol:  protocol,
		UpdatedAt: settings.UpdatedAt,
	}
	response.VLESS.Port = settings.VLESSPort
	response.VLESS.Flow = settings.VLESSFlow
	response.VLESS.Network = settings.VLESSNetwork
	response.Reality.PublicKey = settings.RealityPublicKey
	response.Reality.ShortID = settings.RealityShortID
	response.Reality.ServerName = settings.RealityServerName
	response.Reality.Enabled = settings.RealityPublicKey != ""
	response.WireGuard.Port = settings.WireGuardPort
	response.WireGuard.Address = settings.WireGuardAddress
	response.WireGuard.DNS = settings.WireGuardDNS
	response.WireGuard.PublicKey = settings.WireGuardPublicKey
	response.WireGuard.Ready = settings.WireGuardPublicKey != ""
	return response
}
