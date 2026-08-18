package configs

import (
	"encoding/json"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
	"github.com/ikaevus/routegate/backend/internal/traffic"
)

const (
	singBoxVLESSInboundTag = "vless-in"
	singBoxDirectTag       = "direct"
	singBoxBlockTag        = "block"
	defaultVLESSPort       = 443
	defaultRealityPort     = 443
)

type singBoxVLESSAdapter struct{}

var _ vpnCoreAdapter = singBoxVLESSAdapter{}

func (singBoxVLESSAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{
		Core:          platform.VPNCoreSingBox,
		Protocol:      platform.VPNProtocolVLESS,
		Transports:    []string{platform.VPNTransportTCP},
		SecurityModes: []string{platform.VPNSecurityNone, platform.VPNSecurityReality},
	}
}

func (singBoxVLESSAdapter) Render(config *RenderedConfig, info ServerConfigInfo) {
	config.SingBox = SingBoxConfig{
		Log:      SingBoxLog{Level: "info"},
		Inbounds: []map[string]any{},
		Outbounds: []SingBoxOutbound{{
			Type: "direct",
			Tag:  singBoxDirectTag,
		}},
		Route: SingBoxRoute{
			Rules: []map[string]any{},
			Final: singBoxDirectTag,
		},
	}

	accounts := renderableVPNAccounts(info.VPNAccounts)
	if len(accounts) > 0 {
		users := make([]map[string]any, 0, len(accounts))
		for _, account := range accounts {
			config.VPNAccounts = append(config.VPNAccounts, ConfigVPNAccount{
				ID:          account.ID,
				DisplayName: accountDisplayName(account),
				Status:      account.Status,
				VLESSUUID:   account.VLESSUUID,
			})

			user := map[string]any{
				"uuid": account.VLESSUUID,
				"name": accountDisplayName(account),
			}
			if flow := strings.TrimSpace(account.VLESSFlow); flow != "" {
				user["flow"] = flow
			}
			users = append(users, user)
		}

		inbound := map[string]any{
			"type":        platform.VPNProtocolVLESS,
			"tag":         singBoxVLESSInboundTag,
			"listen":      "::",
			"listen_port": serverVLESSPort(info),
			"users":       users,
		}
		if config.Metadata.RealityEnabled {
			inbound["tls"] = buildRealityTLS(info)
		}
		config.SingBox.Inbounds = append(config.SingBox.Inbounds, inbound)
	}

	applySingBoxRoutingProfile(config, info.RoutingProfile)
}

func (singBoxVLESSAdapter) Validate(config RenderedConfig, result *ValidationResult) {
	if len(config.SingBox.Outbounds) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "singBox.outbounds must contain at least one outbound.")
	}
	if config.SingBox.Route.Final == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "singBox.route.final is required.")
	}
	if len(config.SingBox.Inbounds) == 0 {
		result.Warnings = append(result.Warnings, "No active VPN accounts are available; this config has no VPN listener and cannot be applied.")
	}
	if !config.Metadata.RealityEnabled {
		return
	}
	inbound := findVLESSInbound(config)
	if inbound == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "Reality is enabled but the VLESS inbound is missing.")
		return
	}
	for _, message := range validateRealityInbound(inbound) {
		result.Valid = false
		result.Errors = append(result.Errors, message)
	}
}

func (singBoxVLESSAdapter) Ready(config RenderedConfig) bool {
	inbound := findVLESSInbound(config)
	if inbound == nil {
		return false
	}
	if port := intValue(inbound["listen_port"]); port < 1 || port > 65535 {
		return false
	}
	if sliceLength(inbound["users"]) == 0 {
		return false
	}
	return !config.Metadata.RealityEnabled || len(validateRealityInbound(inbound)) == 0
}

func realityRequested(info ServerConfigInfo) bool {
	return strings.TrimSpace(info.RealityPrivateKey) != "" ||
		strings.TrimSpace(info.RealityPublicKey) != "" ||
		strings.TrimSpace(info.RealityShortID) != "" ||
		strings.TrimSpace(info.RealityServerName) != ""
}

func buildRealityTLS(info ServerConfigInfo) map[string]any {
	serverName := strings.TrimSpace(info.RealityServerName)
	return map[string]any{
		"enabled":     true,
		"server_name": serverName,
		"reality": map[string]any{
			"enabled": true,
			"handshake": map[string]any{
				"server":      serverName,
				"server_port": defaultRealityPort,
			},
			"private_key": strings.TrimSpace(info.RealityPrivateKey),
			"short_id":    []string{strings.TrimSpace(info.RealityShortID)},
		},
	}
}

func applySingBoxRoutingProfile(config *RenderedConfig, profile *RoutingProfileConfigInfo) {
	if profile == nil {
		return
	}
	for _, rule := range profile.Rules {
		outbound, ok := singBoxRoutingOutboundForAction(rule.Action)
		if !ok {
			continue
		}
		routeRule := singBoxRouteRule(rule, outbound)
		if len(routeRule) == 0 {
			continue
		}
		if outbound == singBoxBlockTag {
			ensureSingBoxOutbound(config, SingBoxOutbound{Type: "block", Tag: singBoxBlockTag})
		}
		config.SingBox.Route.Rules = append(config.SingBox.Route.Rules, routeRule)
	}
}

func singBoxRoutingOutboundForAction(action string) (string, bool) {
	switch strings.TrimSpace(action) {
	case routingActionDirect:
		return singBoxDirectTag, true
	case routingActionBlock:
		return singBoxBlockTag, true
	default:
		return "", false
	}
}

func singBoxRouteRule(rule RoutingProfileRuleConfigInfo, outbound string) map[string]any {
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

func ensureSingBoxOutbound(config *RenderedConfig, outbound SingBoxOutbound) {
	for _, existing := range config.SingBox.Outbounds {
		if existing.Tag == outbound.Tag {
			return
		}
	}
	config.SingBox.Outbounds = append(config.SingBox.Outbounds, outbound)
}

func renderableVPNAccounts(accounts []VPNAccountConfigInfo) []VPNAccountConfigInfo {
	renderable := make([]VPNAccountConfigInfo, 0, len(accounts))
	for _, account := range accounts {
		if isVPNAccountRenderable(account) {
			renderable = append(renderable, account)
		}
	}
	return renderable
}

func isVPNAccountRenderable(account VPNAccountConfigInfo) bool {
	if account.Status != "active" || strings.TrimSpace(account.VLESSUUID) == "" {
		return false
	}
	return account.TrafficEnforcementStatus != traffic.TrafficLimitEnforcementOverLimit
}

func accountDisplayName(account VPNAccountConfigInfo) string {
	if displayName := strings.TrimSpace(account.DisplayName); displayName != "" {
		return displayName
	}
	return account.ID
}

func serverVLESSPort(info ServerConfigInfo) int {
	if info.VLESSPort > 0 {
		return info.VLESSPort
	}
	return defaultVLESSPort
}

func validateRealityInbound(inbound map[string]any) []string {
	errorsList := []string{}
	tls, ok := inbound["tls"].(map[string]any)
	if !ok {
		return []string{"Reality is enabled but singBox VLESS TLS settings are missing."}
	}
	if !boolValue(tls["enabled"]) {
		errorsList = append(errorsList, "Reality is enabled but singBox VLESS TLS is disabled.")
	}
	if strings.TrimSpace(stringValue(tls["server_name"])) == "" {
		errorsList = append(errorsList, "Reality server name is required in singBox VLESS TLS settings.")
	}
	reality, ok := tls["reality"].(map[string]any)
	if !ok {
		return append(errorsList, "Reality settings are missing from singBox VLESS TLS settings.")
	}
	if !boolValue(reality["enabled"]) {
		errorsList = append(errorsList, "singBox VLESS Reality must be enabled.")
	}
	if strings.TrimSpace(stringValue(reality["private_key"])) == "" {
		errorsList = append(errorsList, "Reality private key is required for the server config.")
	}
	handshake, ok := reality["handshake"].(map[string]any)
	if !ok {
		errorsList = append(errorsList, "Reality handshake settings are required.")
	} else {
		if strings.TrimSpace(stringValue(handshake["server"])) == "" {
			errorsList = append(errorsList, "Reality handshake server is required.")
		}
		port := intValue(handshake["server_port"])
		if port < 1 || port > 65535 {
			errorsList = append(errorsList, "Reality handshake server port must be between 1 and 65535.")
		}
	}
	if sliceLength(reality["short_id"]) == 0 {
		errorsList = append(errorsList, "Reality short_id must be present in the server config.")
	}
	return errorsList
}

func findVLESSInbound(config RenderedConfig) map[string]any {
	for _, inbound := range config.SingBox.Inbounds {
		if strings.EqualFold(strings.TrimSpace(stringValue(inbound["type"])), platform.VPNProtocolVLESS) {
			return inbound
		}
	}
	return nil
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		value, err := typed.Int64()
		if err == nil {
			return int(value)
		}
	}
	return 0
}

func sliceLength(value any) int {
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	case []string:
		return len(typed)
	default:
		return 0
	}
}
