package tasks

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

const hysteria2ValidationTimeout = 10 * time.Second
const hysteria2ACMEDir = "/var/lib/hysteria/acme"

type hysteria2Adapter struct {
	stagingDir  string
	binary      string
	ssPath      string
	service     ServiceController
	run         commandRunner
}

var _ VPNCoreAdapter = hysteria2Adapter{}

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
	Type  string                         `json:"type"`
	Proxy hysteria2MasqueradeProxyConfig `json:"proxy"`
}

type hysteria2MasqueradeProxyConfig struct {
	URL         string `json:"url"`
	RewriteHost bool   `json:"rewriteHost"`
	Insecure    bool   `json:"insecure"`
	XForwarded  bool   `json:"xForwarded"`
}

func NewHysteria2Adapter(stagingDir, binary, ssPath, serviceName string) VPNCoreAdapter {
	return hysteria2Adapter{
		stagingDir: strings.TrimSpace(stagingDir),
		binary:     defaultTaskValue(binary, "hysteria"),
		ssPath:     defaultTaskValue(ssPath, "ss"),
		service:    NewServiceController(defaultTaskValue(serviceName, "hysteria-server")),
		run:        runCommand,
	}
}

func (hysteria2Adapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.ManagedVPNCoreAdapters()[2]
}

func (a hysteria2Adapter) Stage(task ConfigTask) (StageResult, error) {
	if task.EffectiveKind() != TaskKindConfigApply || task.ID == "" || task.ConfigVersionID == "" {
		return StageResult{}, errors.New("valid Hysteria2 config apply task identifiers are required")
	}
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
		Hysteria2     string `json:"hysteria2"`
	}
	if err := json.Unmarshal(task.RenderedConfig, &envelope); err != nil {
		return StageResult{}, fmt.Errorf("decode rendered config envelope: %w", err)
	}
	if envelope.SchemaVersion != "routegate.config.v1" || strings.TrimSpace(envelope.Hysteria2) == "" {
		return StageResult{}, errors.New("rendered config envelope must contain Hysteria2 config")
	}
	if _, err := parseHysteria2Config(envelope.Hysteria2); err != nil {
		return StageResult{}, err
	}
	if err := os.MkdirAll(a.stagingDir, 0o750); err != nil {
		return StageResult{}, fmt.Errorf("create Hysteria2 staging dir: %w", err)
	}
	path := filepath.Join(a.stagingDir, task.ConfigVersionID+".json")
	tmpPath := path + ".tmp"
	content := []byte(strings.TrimSpace(envelope.Hysteria2) + "\n")
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return StageResult{}, fmt.Errorf("write staged Hysteria2 config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return StageResult{}, fmt.Errorf("commit staged Hysteria2 config: %w", err)
	}
	return StageResult{StagedPath: path, ConfigHash: task.ConfigHash, ConfigVersionID: task.ConfigVersionID}, nil
}

func (a hysteria2Adapter) Validate(ctx context.Context, configPath string) (ValidationResult, error) {
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return ValidationResult{}, fmt.Errorf("read staged Hysteria2 config: %w", err)
	}
	if _, err := parseHysteria2Config(string(payload)); err != nil {
		return ValidationResult{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, hysteria2ValidationTimeout)
	defer cancel()
	output, err := a.run(checkCtx, a.binary, "version")
	result := ValidationResult{Command: a.binary + " version", Output: strings.TrimSpace(string(output))}
	if err != nil {
		if checkCtx.Err() != nil {
			return result, fmt.Errorf("Hysteria2 binary validation timed out: %w", checkCtx.Err())
		}
		return result, errors.New("Hysteria2 binary is unavailable or failed its version check")
	}
	return result, nil
}

func (a hysteria2Adapter) Restart(ctx context.Context) (ServiceResult, error) { return a.service.Restart(ctx) }
func (a hysteria2Adapter) IsActive(ctx context.Context) (ServiceResult, error) { return a.service.IsActive(ctx) }
func (a hysteria2Adapter) IsEnabled(ctx context.Context) (ServiceResult, error) { return a.service.IsEnabled(ctx) }
func (a hysteria2Adapter) ExecuteServiceTask(ctx context.Context, task ConfigTask) (ServiceTaskReport, error) {
	return ExecuteServiceTask(ctx, a.service, task)
}

func (a hysteria2Adapter) CheckHealth(ctx context.Context, configPath string) (ListenerHealthResult, error) {
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return ListenerHealthResult{}, errors.New("read active Hysteria2 config failed")
	}
	config, err := parseHysteria2Config(string(payload))
	if err != nil {
		return ListenerHealthResult{}, err
	}
	port, _ := strconv.Atoi(strings.TrimPrefix(config.Listen, ":"))
	checkCtx, cancel := context.WithTimeout(ctx, defaultListenerHealthTimeout)
	defer cancel()
	output, err := a.run(checkCtx, a.ssPath, "-H", "-lunp")
	result := ListenerHealthResult{Address: "udp", Port: port}
	if err != nil {
		return result, errors.New("Hysteria2 UDP listener health check failed")
	}
	lineMatch := false
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, ":"+strconv.Itoa(port)) && strings.Contains(strings.ToLower(line), "hysteria") {
			lineMatch = true
			break
		}
	}
	if !lineMatch {
		return result, errors.New("Hysteria2 process did not report the configured UDP listener")
	}
	return result, nil
}

func parseHysteria2Config(payload string) (hysteria2ServerConfig, error) {
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
	if config.Auth.Type != "userpass" || len(config.Auth.Userpass) == 0 {
		return config, errors.New("Hysteria2 userpass authentication requires at least one account")
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
	if len(value) != 36 { return false }
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
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == 24
}

func validHysteria2MasqueradeURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.String() == "https://www.cloudflare.com/" && parsed.User == nil && parsed.Fragment == ""
}
