package configs

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

const (
	shadowsocksInboundTag  = "shadowsocks-in"
	shadowsocksMethod      = "2022-blake3-aes-128-gcm"
	defaultShadowsocksPort = 8388
)

type shadowsocksAdapter struct{}

var _ vpnCoreAdapter = shadowsocksAdapter{}

func (shadowsocksAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{
		Core:          platform.VPNCoreSingBox,
		Protocol:      platform.VPNProtocolShadowsocks,
		Transports:    []string{platform.VPNTransportTCP},
		SecurityModes: []string{platform.VPNSecurityAEAD2022},
	}
}

func (shadowsocksAdapter) Render(config *RenderedConfig, info ServerConfigInfo) {
	ensureSingBoxBase(config)

	users := make([]map[string]any, 0, len(info.VPNAccounts))
	for _, account := range info.VPNAccounts {
		if !isShadowsocksAccountRenderable(account) {
			continue
		}
		username := strings.ToLower(strings.TrimSpace(account.ID))
		users = append(users, map[string]any{
			"name":     username,
			"password": strings.TrimSpace(account.ShadowsocksUserKey),
		})
		config.VPNAccounts = append(config.VPNAccounts, ConfigVPNAccount{
			ID:                  account.ID,
			DisplayName:         accountDisplayName(account),
			Status:              account.Status,
			Protocol:            platform.VPNProtocolShadowsocks,
			ShadowsocksUsername: username,
		})
	}

	if len(users) > 0 {
		port := info.ShadowsocksPort
		if port == 0 {
			port = defaultShadowsocksPort
		}
		config.SingBox.Inbounds = append(config.SingBox.Inbounds, map[string]any{
			"type":        platform.VPNProtocolShadowsocks,
			"tag":         shadowsocksInboundTag,
			"listen":      "::",
			"listen_port": port,
			"network":     platform.VPNTransportTCP,
			"method":      shadowsocksMethod,
			"password":    strings.TrimSpace(info.ShadowsocksServerKey),
			"users":       users,
			"multiplex": map[string]any{
				"enabled": true,
				"padding": false,
			},
		})
	}
	applySingBoxRoutingProfile(config, info.RoutingProfile)
}

func (shadowsocksAdapter) Validate(config RenderedConfig, result *ValidationResult) {
	inbound, err := parseShadowsocksInbound(config)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return
	}
	if inbound == nil {
		result.Warnings = append(result.Warnings, "No active Shadowsocks accounts are available; this config cannot be applied.")
	}
}

func (shadowsocksAdapter) Ready(config RenderedConfig) bool {
	inbound, err := parseShadowsocksInbound(config)
	return err == nil && inbound != nil
}

func isShadowsocksAccountRenderable(account VPNAccountConfigInfo) bool {
	return accountUsesProtocol(account, platform.VPNProtocolShadowsocks) && account.Status == "active" && validShadowsocksKey(account.ShadowsocksUserKey) && account.TrafficEnforcementStatus != "over_limit"
}

func parseShadowsocksInbound(config RenderedConfig) (map[string]any, error) {
	if len(config.SingBox.Outbounds) == 0 || config.SingBox.Route.Final != singBoxDirectTag {
		return nil, errors.New("Shadowsocks sing-box routing must contain the fixed direct path")
	}
	var selected map[string]any
	for _, inbound := range config.SingBox.Inbounds {
		if strings.TrimSpace(stringValueFromMap(inbound, "type")) != platform.VPNProtocolShadowsocks {
			continue
		}
		if selected != nil {
			return nil, errors.New("Shadowsocks config must contain at most one inbound")
		}
		selected = inbound
	}
	if selected == nil {
		return nil, nil
	}
	if stringValueFromMap(selected, "tag") != shadowsocksInboundTag || stringValueFromMap(selected, "listen") != "::" ||
		stringValueFromMap(selected, "network") != platform.VPNTransportTCP || stringValueFromMap(selected, "method") != shadowsocksMethod {
		return nil, errors.New("Shadowsocks inbound must match the fixed RouteGate AEAD-2022/TCP policy")
	}
	port := intValue(selected["listen_port"])
	if port < 1 || port > 65535 {
		return nil, errors.New("Shadowsocks listen_port must be between 1 and 65535")
	}
	if !validShadowsocksKey(stringValueFromMap(selected, "password")) {
		return nil, errors.New("Shadowsocks server key must contain 16 random bytes in base64")
	}
	multiplex, ok := selected["multiplex"].(map[string]any)
	if !ok || multiplex["enabled"] != true || multiplex["padding"] != false || len(multiplex) != 2 {
		return nil, errors.New("Shadowsocks multiplex settings must match the fixed RouteGate policy")
	}
	users, ok := mapSlice(selected["users"])
	if !ok || len(users) == 0 {
		return nil, errors.New("Shadowsocks inbound must contain at least one active user")
	}
	seenNames := map[string]struct{}{}
	seenKeys := map[string]struct{}{}
	for _, user := range users {
		name := stringValueFromMap(user, "name")
		key := stringValueFromMap(user, "password")
		if !validHysteria2Username(name) || !validShadowsocksKey(key) || len(user) != 2 {
			return nil, errors.New("Shadowsocks user credentials are invalid")
		}
		if _, exists := seenNames[name]; exists {
			return nil, errors.New("Shadowsocks usernames must be unique")
		}
		if _, exists := seenKeys[key]; exists {
			return nil, errors.New("Shadowsocks user keys must be unique")
		}
		seenNames[name] = struct{}{}
		seenKeys[key] = struct{}{}
	}
	return selected, nil
}

func validShadowsocksKey(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == 16
}

func stringValueFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func mapSlice(value any) ([]map[string]any, bool) {
	if values, ok := value.([]map[string]any); ok {
		return values, true
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	values := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		mapped, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		values = append(values, mapped)
	}
	return values, true
}
