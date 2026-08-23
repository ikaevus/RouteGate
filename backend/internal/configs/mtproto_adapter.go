package configs

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

const (
	defaultMTProtoPort        = 9443
	mtprotoFrontingDomain     = "www.cloudflare.com"
	mtprotoDefaultConcurrency = 8192
)

type mtprotoAdapter struct{}

var _ vpnCoreAdapter = mtprotoAdapter{}

type mtprotoServerConfig struct {
	Secret         string
	Port           int
	FrontingDomain string
}

func (mtprotoAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{
		Core:          platform.VPNCoreMTG,
		Protocol:      platform.VPNProtocolMTProto,
		Transports:    []string{platform.VPNTransportTCP},
		SecurityModes: []string{platform.VPNSecurityFakeTLS},
	}
}

func (mtprotoAdapter) Render(config *RenderedConfig, info ServerConfigInfo) {
	port := info.MTProtoPort
	if port == 0 {
		port = defaultMTProtoPort
	}
	var rendered strings.Builder
	rendered.WriteString("debug = false\n")
	fmt.Fprintf(&rendered, "secret = %q\n", strings.ToLower(strings.TrimSpace(info.MTProtoSecret)))
	fmt.Fprintf(&rendered, "bind-to = %q\n", "0.0.0.0:"+strconv.Itoa(port))
	fmt.Fprintf(&rendered, "concurrency = %d\n", mtprotoDefaultConcurrency)
	rendered.WriteString("prefer-ip = \"prefer-ipv4\"\n")
	rendered.WriteString("auto-update = false\n")
	config.MTProto = rendered.String()
}

func (mtprotoAdapter) Validate(config RenderedConfig, result *ValidationResult) {
	if _, err := parseMTProtoServerConfig(config.MTProto); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}
}

func (mtprotoAdapter) Ready(config RenderedConfig) bool {
	_, err := parseMTProtoServerConfig(config.MTProto)
	return err == nil
}

func parseMTProtoServerConfig(payload string) (mtprotoServerConfig, error) {
	parsed := mtprotoServerConfig{FrontingDomain: mtprotoFrontingDomain}
	values := map[string]string{}
	for _, rawLine := range strings.Split(strings.TrimSpace(payload), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return parsed, errors.New("MTProto config must use the fixed RouteGate TOML grammar")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if _, allowed := map[string]bool{"debug": true, "secret": true, "bind-to": true, "concurrency": true, "prefer-ip": true, "auto-update": true}[key]; !allowed {
			return parsed, fmt.Errorf("MTProto field %q is not allowed", key)
		}
		if _, duplicate := values[key]; duplicate {
			return parsed, fmt.Errorf("MTProto field %q is duplicated", key)
		}
		values[key] = value
	}
	if len(values) != 6 || values["debug"] != "false" || values["concurrency"] != strconv.Itoa(mtprotoDefaultConcurrency) ||
		values["prefer-ip"] != "\"prefer-ipv4\"" || values["auto-update"] != "false" {
		return parsed, errors.New("MTProto config must match the fixed RouteGate runtime policy")
	}
	secret, err := strconv.Unquote(values["secret"])
	if err != nil || !validMTProtoSecret(secret) {
		return parsed, errors.New("MTProto FakeTLS secret is invalid")
	}
	bindTo, err := strconv.Unquote(values["bind-to"])
	if err != nil || !strings.HasPrefix(bindTo, "0.0.0.0:") {
		return parsed, errors.New("MTProto bind-to must listen on one fixed TCP port")
	}
	port, err := strconv.Atoi(strings.TrimPrefix(bindTo, "0.0.0.0:"))
	if err != nil || port < 1 || port > 65535 || bindTo != "0.0.0.0:"+strconv.Itoa(port) {
		return parsed, errors.New("MTProto bind-to port must be between 1 and 65535")
	}
	parsed.Secret = strings.ToLower(secret)
	parsed.Port = port
	return parsed, nil
}

func validMTProtoSecret(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 70 || !strings.HasPrefix(value, "ee") {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 35 {
		return false
	}
	return string(decoded[17:]) == mtprotoFrontingDomain
}
