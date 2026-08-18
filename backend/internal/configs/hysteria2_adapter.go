package configs

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

const hysteria2ACMEDir = "/var/lib/hysteria/acme"

type hysteria2Adapter struct{}

var _ vpnCoreAdapter = hysteria2Adapter{}

type hysteria2ServerConfig struct {
	Listen     string                    `json:"listen"`
	ACME       hysteria2ACMEConfig       `json:"acme"`
	Auth       hysteria2AuthConfig       `json:"auth"`
	Masquerade hysteria2MasqueradeConfig `json:"masquerade"`
}

type hysteria2ACMEConfig struct {
	Domains []string `json:"domains"`
	Email   string   `json:"email"`
	CA      string   `json:"ca"`
	Dir     string   `json:"dir"`
	Type    string   `json:"type"`
}

type hysteria2AuthConfig struct {
	Type     string            `json:"type"`
	Userpass map[string]string `json:"userpass"`
}

type hysteria2MasqueradeConfig struct {
	Type  string                       `json:"type"`
	Proxy hysteria2MasqueradeProxyConfig `json:"proxy"`
}

type hysteria2MasqueradeProxyConfig struct {
	URL         string `json:"url"`
	RewriteHost bool   `json:"rewriteHost"`
	Insecure    bool   `json:"insecure"`
	XForwarded  bool   `json:"xForwarded"`
}

func (hysteria2Adapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{
		Core:          platform.VPNCoreHysteria,
		Protocol:      platform.VPNProtocolHysteria2,
		Transports:    []string{platform.VPNTransportQUIC},
		SecurityModes: []string{platform.VPNSecurityTLS},
	}
}

func (hysteria2Adapter) Render(config *RenderedConfig, info ServerConfigInfo) {
	port := info.Hysteria2Port
	if port == 0 {
		port = 443
	}
	payload := hysteria2ServerConfig{
		Listen: ":" + strconv.Itoa(port),
		ACME: hysteria2ACMEConfig{
			Domains: []string{strings.ToLower(strings.TrimSpace(info.Hysteria2Domain))},
			Email:   strings.TrimSpace(info.Hysteria2ACMEEmail),
			CA:      "letsencrypt",
			Dir:     hysteria2ACMEDir,
			Type:    "http",
		},
		Auth: hysteria2AuthConfig{Type: "userpass", Userpass: map[string]string{}},
		Masquerade: hysteria2MasqueradeConfig{
			Type: "proxy",
			Proxy: hysteria2MasqueradeProxyConfig{
				URL: strings.TrimSpace(info.Hysteria2MasqueradeURL), RewriteHost: true,
				Insecure: false, XForwarded: false,
			},
		},
	}
	for _, account := range info.VPNAccounts {
		if !isHysteria2AccountRenderable(account) {
			continue
		}
		username := strings.ToLower(strings.TrimSpace(account.ID))
		payload.Auth.Userpass[username] = strings.TrimSpace(account.Hysteria2Password)
		config.VPNAccounts = append(config.VPNAccounts, ConfigVPNAccount{
			ID: account.ID, DisplayName: accountDisplayName(account), Status: account.Status,
			Hysteria2Username: username,
		})
	}
	rendered, err := json.MarshalIndent(payload, "", "  ")
	if err == nil {
		config.Hysteria2 = string(rendered)
	}
}

func (hysteria2Adapter) Validate(config RenderedConfig, result *ValidationResult) {
	if config.Server.DeploymentRole != string(platform.DeploymentRoleVPN) {
		result.Valid = false
		result.Errors = append(result.Errors, "Hysteria2 ACME is supported only on a dedicated VPN Node in RG-114F.")
		return
	}
	parsed, err := parseHysteria2ServerConfig(config.Hysteria2)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return
	}
	if len(parsed.Auth.Userpass) == 0 {
		result.Warnings = append(result.Warnings, "No active Hysteria2 accounts are available; this config cannot be applied.")
	}
}

func (hysteria2Adapter) Ready(config RenderedConfig) bool {
	parsed, err := parseHysteria2ServerConfig(config.Hysteria2)
	return err == nil && len(parsed.Auth.Userpass) > 0
}

func isHysteria2AccountRenderable(account VPNAccountConfigInfo) bool {
	return account.Status == "active" && validHysteria2Password(account.Hysteria2Password) && account.TrafficEnforcementStatus != "over_limit"
}

func parseHysteria2ServerConfig(payload string) (hysteria2ServerConfig, error) {
	var config hysteria2ServerConfig
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, errors.New("Hysteria2 config must use the supported strict JSON schema")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return config, errors.New("Hysteria2 config must contain one JSON object")
	}
	port, err := strconv.Atoi(strings.TrimPrefix(config.Listen, ":"))
	if err != nil || port < 1 || port > 65535 || config.Listen != ":"+strconv.Itoa(port) {
		return config, errors.New("Hysteria2 listen must contain one UDP port")
	}
	if len(config.ACME.Domains) != 1 || !validHysteria2Domain(config.ACME.Domains[0]) {
		return config, errors.New("Hysteria2 ACME requires one valid DNS domain")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(config.ACME.Email))
	if err != nil || address.Address != strings.TrimSpace(config.ACME.Email) {
		return config, errors.New("Hysteria2 ACME email is invalid")
	}
	if config.ACME.CA != "letsencrypt" || config.ACME.Dir != hysteria2ACMEDir || config.ACME.Type != "http" {
		return config, errors.New("Hysteria2 ACME settings must match the fixed RouteGate policy")
	}
	if config.Auth.Type != "userpass" {
		return config, errors.New("Hysteria2 authentication must use the fixed userpass mode")
	}
	for username, password := range config.Auth.Userpass {
		if !validHysteria2Username(username) || !validHysteria2Password(password) {
			return config, errors.New("Hysteria2 account credentials are invalid")
		}
	}
	if config.Masquerade.Type != "proxy" || !validHysteria2MasqueradeURL(config.Masquerade.Proxy.URL) ||
		!config.Masquerade.Proxy.RewriteHost || config.Masquerade.Proxy.Insecure || config.Masquerade.Proxy.XForwarded {
		return config, errors.New("Hysteria2 masquerade must match the fixed safe proxy policy")
	}
	return config, nil
}

func validHysteria2Domain(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	labels := strings.Split(value, ".")
	if len(value) < 4 || len(value) > 253 || len(labels) < 2 { return false }
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' { return false }
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') { return false }
		}
	}
	return true
}

func validHysteria2Username(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' { return false }
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) { return false }
	}
	return true
}

func validHysteria2Password(value string) bool {
	value = strings.TrimSpace(value)
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 24
}

func validHysteria2MasqueradeURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.String() == "https://www.cloudflare.com/" && parsed.User == nil && parsed.Fragment == ""
}
