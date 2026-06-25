package vpnaccounts

import "fmt"

const (
	ClientConfigType    = "routegate-client-config"
	ClientConfigVersion = "routegate.client_config.v1"
)

func BuildClientConfig(profile SubscriptionProfile) PublicSubscriptionConfig {
	server := publicSubscriptionServer(profile.Server)
	if server == nil || server.Endpoint == "" {
		return PublicSubscriptionConfig{
			Type:    ClientConfigType,
			Status:  "pending",
			Message: "VPN account is active, but no server endpoint is assigned yet.",
		}
	}

	return PublicSubscriptionConfig{
		Type:   ClientConfigType,
		Status: "ready",
		Payload: &ClientConfigPayload{
			Version:     ClientConfigVersion,
			ProfileName: clientConfigProfileName(profile.Account, server),
			Account: ClientConfigAccount{
				ID:          profile.Account.ID,
				DisplayName: profile.Account.DisplayName,
				ExpiresAt:   profile.Account.ExpiresAt,
				MaxDevices:  profile.Account.MaxDevices,
			},
			Server: ClientConfigServer{
				ID:       server.ID,
				Name:     server.Name,
				Hostname: server.Hostname,
				PublicIP: server.PublicIP,
				Endpoint: server.Endpoint,
				Location: server.Location,
				Provider: server.Provider,
			},
			Protocol: ClientConfigProtocol{
				Engine: "sing-box",
				Status: "format-renderer-pending",
				Message: "RG-41 returns a stable RouteGate client config envelope; concrete sing-box and Clash renderers will fill protocol credentials later.",
			},
		},
	}
}

func clientConfigProfileName(account Account, server *PublicSubscriptionServer) string {
	if server != nil && server.Name != "" {
		return fmt.Sprintf("%s - %s", account.DisplayName, server.Name)
	}
	return account.DisplayName
}
