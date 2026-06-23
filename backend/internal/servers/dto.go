package servers

import (
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

type CreateServerRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Provider    string `json:"provider"`
	PublicIP    string `json:"publicIp"`
	PrivateIP   string `json:"privateIp"`

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

type ServerResponse struct {
	Server
	Agent *agents.Agent `json:"agent,omitempty"`
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
}

func newServerResponse(server ServerWithAgent) ServerResponse {
	return ServerResponse{
		Server: server.Server,
		Agent:  server.Agent,
	}
}
