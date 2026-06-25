package vpnaccounts

import (
	"errors"
	"strings"
)

const (
	ClientConfigFormat        = "routegate.client_config.v1"
	SingBoxClientConfigFormat = "sing-box.config.v1"

	singBoxOutboundTag = "routegate-out"
	singBoxInboundTag  = "mixed-in"
)

const (
	defaultSingBoxServerPort = 443
	defaultMixedListenPort   = 2080
)

type SingBoxClientConfig struct {
	Log       *SingBoxLog        `json:"log,omitempty"`
	Inbounds  []SingBoxInbound   `json:"inbounds"`
	Outbounds []SingBoxOutbound  `json:"outbounds"`
	Route     SingBoxRoute       `json:"route"`
}

type SingBoxLog struct {
	Level     string `json:"level"`
	Timestamp bool   `json:"timestamp"`
}

type SingBoxInbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
}

type SingBoxOutbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	UUID       string `json:"uuid,omitempty"`
	Network    string `json:"network,omitempty"`
}

type SingBoxRoute struct {
	Final string `json:"final"`
}

func renderPublicSubscriptionConfig(profile SubscriptionProfile) PublicSubscriptionConfig {
	config := PublicSubscriptionConfig{
		Type:   "sing-box",
		Format: ClientConfigFormat,
	}

	rendered, err := RenderSingBoxClientConfig(profile)
	if err != nil {
		config.Status = "unavailable"
		config.Message = err.Error()
		return config
	}

	config.Status = "rendered"
	config.Message = "Minimal sing-box client config generated."
	config.Rendered = &PublicSubscriptionRenderedConfig{
		Format:  SingBoxClientConfigFormat,
		Content: rendered,
	}
	return config
}

func RenderSingBoxClientConfig(profile SubscriptionProfile) (SingBoxClientConfig, error) {
	accountID := strings.TrimSpace(profile.Account.ID)
	if accountID == "" {
		return SingBoxClientConfig{}, errors.New("VPN account ID is required to render sing-box client config.")
	}

	endpoint := subscriptionServerEndpoint(profile.Server)
	if endpoint == "" {
		return SingBoxClientConfig{}, errors.New("Server endpoint is required to render sing-box client config.")
	}

	return SingBoxClientConfig{
		Log: &SingBoxLog{
			Level:     "info",
			Timestamp: true,
		},
		Inbounds: []SingBoxInbound{
			{
				Type:       "mixed",
				Tag:        singBoxInboundTag,
				Listen:     "127.0.0.1",
				ListenPort: defaultMixedListenPort,
			},
		},
		Outbounds: []SingBoxOutbound{
			{
				Type:       "vless",
				Tag:        singBoxOutboundTag,
				Server:     endpoint,
				ServerPort: defaultSingBoxServerPort,
				UUID:       accountID,
				Network:    "tcp",
			},
			{
				Type: "direct",
				Tag:  "direct",
			},
		},
		Route: SingBoxRoute{Final: singBoxOutboundTag},
	}, nil
}

func subscriptionServerEndpoint(server *SubscriptionServer) string {
	if server == nil {
		return ""
	}
	if endpoint := strings.TrimSpace(server.PublicIP); endpoint != "" {
		return endpoint
	}
	return strings.TrimSpace(server.Hostname)
}
