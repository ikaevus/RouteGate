package vpnaccounts

import (
	"errors"
	"net/netip"
	"strings"
)

const (
	ClientConfigFormat        = "routegate.client_config.v1"
	SingBoxClientConfigFormat = "sing-box.config.v1"

	singBoxOutboundTag = "routegate-out"
	singBoxInboundTag  = "mixed-in"
	singBoxDirectTag   = "direct"
	singBoxBlockTag    = "block"
)

const (
	defaultSingBoxServerPort = 443
	defaultMixedListenPort   = 2080
)

type SingBoxClientConfig struct {
	Log       *SingBoxLog       `json:"log,omitempty"`
	Inbounds  []SingBoxInbound  `json:"inbounds"`
	Outbounds []SingBoxOutbound `json:"outbounds"`
	Route     SingBoxRoute      `json:"route"`
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
	Type       string      `json:"type"`
	Tag        string      `json:"tag"`
	Server     string      `json:"server,omitempty"`
	ServerPort int         `json:"server_port,omitempty"`
	UUID       string      `json:"uuid,omitempty"`
	Flow       string      `json:"flow,omitempty"`
	Network    string      `json:"network,omitempty"`
	TLS        *SingBoxTLS `json:"tls,omitempty"`
}

type SingBoxTLS struct {
	Enabled    bool               `json:"enabled"`
	ServerName string             `json:"server_name,omitempty"`
	Reality    *SingBoxTLSReality `json:"reality,omitempty"`
}

type SingBoxTLSReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
}

type SingBoxRoute struct {
	Rules []map[string]any `json:"rules,omitempty"`
	Final string           `json:"final"`
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
	vlessUUID := subscriptionVLESSUUID(profile)
	if vlessUUID == "" {
		return SingBoxClientConfig{}, errors.New("VLESS UUID is required to render sing-box client config.")
	}

	endpoint := subscriptionServerEndpoint(profile.Server)
	if endpoint == "" {
		return SingBoxClientConfig{}, errors.New("Server endpoint is required to render sing-box client config.")
	}

	vlessOutbound := SingBoxOutbound{
		Type:       "vless",
		Tag:        singBoxOutboundTag,
		Server:     endpoint,
		ServerPort: subscriptionServerPort(profile.Server),
		UUID:       vlessUUID,
		Network:    subscriptionVLESSNetwork(profile),
		TLS:        subscriptionTLS(profile, endpoint),
	}
	if flow := subscriptionVLESSFlow(profile); flow != "" {
		vlessOutbound.Flow = flow
	}

	routeRules, needsBlockOutbound := renderClientRoutingRules(profile.RoutingProfile)
	outbounds := []SingBoxOutbound{
		vlessOutbound,
		{
			Type: "direct",
			Tag:  singBoxDirectTag,
		},
	}
	if needsBlockOutbound {
		outbounds = append(outbounds, SingBoxOutbound{Type: "block", Tag: singBoxBlockTag})
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
		Outbounds: outbounds,
		Route: SingBoxRoute{
			Rules: routeRules,
			Final: singBoxOutboundTag,
		},
	}, nil
}

func renderClientRoutingRules(profile *RoutingProfile) ([]map[string]any, bool) {
	if profile == nil {
		return nil, false
	}

	rules := make([]map[string]any, 0, len(profile.Rules))
	needsBlockOutbound := false
	for _, rule := range profile.Rules {
		outbound := clientRoutingOutboundForAction(rule.Action)
		if outbound == "" {
			continue
		}
		rendered := singBoxRouteRule(rule, outbound)
		if len(rendered) == 0 {
			continue
		}
		if outbound == singBoxBlockTag {
			needsBlockOutbound = true
		}
		rules = append(rules, rendered)
	}
	return rules, needsBlockOutbound
}

func clientRoutingOutboundForAction(action string) string {
	switch strings.TrimSpace(action) {
	case RoutingActionDirect:
		return singBoxDirectTag
	case RoutingActionVPN:
		return singBoxOutboundTag
	case RoutingActionBlock:
		return singBoxBlockTag
	default:
		return ""
	}
}

func singBoxRouteRule(rule RoutingProfileRule, outbound string) map[string]any {
	rendered := map[string]any{"outbound": outbound}
	addStringList(rendered, "domain", rule.Domains)
	addStringList(rendered, "domain_suffix", rule.DomainSuffixes)
	addStringList(rendered, "domain_keyword", rule.DomainKeywords)
	addStringList(rendered, "ip_cidr", rule.IPCIDRs)
	addStringList(rendered, "geosite", rule.GeoSites)
	addStringList(rendered, "geoip", rule.GeoIPs)
	if len(rendered) == 1 {
		return map[string]any{}
	}
	return rendered
}

func addStringList(target map[string]any, key string, values []string) {
	cleaned := cleanStrings(values)
	if len(cleaned) > 0 {
		target[key] = cleaned
	}
}

func cleanStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func subscriptionVLESSUUID(profile SubscriptionProfile) string {
	if uuid := strings.TrimSpace(profile.Credentials.VLESS.UUID); uuid != "" {
		return uuid
	}
	return strings.TrimSpace(profile.Account.VLESSUUID)
}

func subscriptionVLESSFlow(profile SubscriptionProfile) string {
	if flow := strings.TrimSpace(profile.Credentials.VLESS.Flow); flow != "" {
		return flow
	}
	if profile.Server == nil {
		return ""
	}
	return strings.TrimSpace(profile.Server.VLESSFlow)
}

func subscriptionVLESSNetwork(profile SubscriptionProfile) string {
	if network := strings.TrimSpace(profile.Credentials.VLESS.Network); network != "" {
		return network
	}
	if profile.Server != nil {
		if network := strings.TrimSpace(profile.Server.VLESSNetwork); network != "" {
			return network
		}
	}
	return "tcp"
}

func subscriptionTLS(profile SubscriptionProfile, endpoint string) *SingBoxTLS {
	reality := profile.Credentials.Reality
	if strings.TrimSpace(reality.PublicKey) == "" && profile.Server != nil {
		reality = RealityCredentials{
			PublicKey:  profile.Server.RealityPublicKey,
			ShortID:    profile.Server.RealityShortID,
			ServerName: profile.Server.RealityServerName,
		}
	}

	publicKey := strings.TrimSpace(reality.PublicKey)
	if publicKey == "" {
		return nil
	}

	serverName := strings.TrimSpace(reality.ServerName)
	if serverName == "" {
		serverName = endpoint
	}

	return &SingBoxTLS{
		Enabled:    true,
		ServerName: serverName,
		Reality: &SingBoxTLSReality{
			Enabled:   true,
			PublicKey: publicKey,
			ShortID:   strings.TrimSpace(reality.ShortID),
		},
	}
}

func subscriptionServerPort(server *SubscriptionServer) int {
	if server != nil && server.VLESSPort > 0 {
		return server.VLESSPort
	}
	return defaultSingBoxServerPort
}

func subscriptionServerEndpoint(server *SubscriptionServer) string {
	if server == nil {
		return ""
	}
	if endpoint := strings.TrimSpace(server.PublicIP); endpoint != "" {
		return normalizeServerEndpoint(endpoint)
	}
	return normalizeServerEndpoint(server.Hostname)
}

func normalizeServerEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	prefix, err := netip.ParsePrefix(endpoint)
	if err == nil && prefix.Bits() == prefix.Addr().BitLen() {
		return prefix.Addr().String()
	}
	return endpoint
}
