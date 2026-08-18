package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

const wireGuardValidationTimeout = 10 * time.Second

type wireGuardAdapter struct {
	stagingDir string
	wgQuickPath string
	wgPath string
	interfaceName string
	service ServiceController
	run commandRunner
}

var _ VPNCoreAdapter = wireGuardAdapter{}

func NewWireGuardAdapter(stagingDir, wgQuickPath, wgPath, serviceName, interfaceName string) VPNCoreAdapter {
	return wireGuardAdapter{
		stagingDir: strings.TrimSpace(stagingDir),
		wgQuickPath: defaultTaskValue(wgQuickPath, "wg-quick"),
		wgPath: defaultTaskValue(wgPath, "wg"),
		interfaceName: defaultTaskValue(interfaceName, "routegate-wg0"),
		service: NewServiceController(defaultTaskValue(serviceName, "wg-quick@routegate-wg0")),
		run: runCommand,
	}
}

func (wireGuardAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.ManagedVPNCoreAdapters()[1]
}

func (a wireGuardAdapter) Stage(task ConfigTask) (StageResult, error) {
	if task.EffectiveKind() != TaskKindConfigApply || task.ID == "" || task.ConfigVersionID == "" {
		return StageResult{}, errors.New("valid WireGuard config apply task identifiers are required")
	}
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
		WireGuard string `json:"wireGuard"`
	}
	if err := json.Unmarshal(task.RenderedConfig, &envelope); err != nil {
		return StageResult{}, fmt.Errorf("decode rendered config envelope: %w", err)
	}
	if envelope.SchemaVersion != "routegate.config.v1" || strings.TrimSpace(envelope.WireGuard) == "" {
		return StageResult{}, errors.New("rendered config envelope must contain WireGuard config")
	}
	if _, err := parseWireGuardConfig(envelope.WireGuard); err != nil {
		return StageResult{}, err
	}
	if err := os.MkdirAll(a.stagingDir, 0o750); err != nil {
		return StageResult{}, fmt.Errorf("create WireGuard staging dir: %w", err)
	}
	path := filepath.Join(a.stagingDir, task.ConfigVersionID+".conf")
	tmpPath := path + ".tmp"
	content := []byte(strings.TrimSpace(envelope.WireGuard) + "\n")
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return StageResult{}, fmt.Errorf("write staged WireGuard config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return StageResult{}, fmt.Errorf("commit staged WireGuard config: %w", err)
	}
	return StageResult{StagedPath: path, ConfigHash: task.ConfigHash, ConfigVersionID: task.ConfigVersionID}, nil
}

func (a wireGuardAdapter) Validate(ctx context.Context, configPath string) (ValidationResult, error) {
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return ValidationResult{}, fmt.Errorf("read staged WireGuard config: %w", err)
	}
	if _, err := parseWireGuardConfig(string(payload)); err != nil {
		return ValidationResult{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, wireGuardValidationTimeout)
	defer cancel()
	args := []string{"strip", configPath}
	_, err = a.run(checkCtx, a.wgQuickPath, args...)
	result := ValidationResult{Command: a.wgQuickPath + " " + strings.Join(args, " ")}
	if err != nil {
		if checkCtx.Err() != nil {
			return result, fmt.Errorf("wg-quick validation timed out: %w", checkCtx.Err())
		}
		return result, errors.New("wg-quick rejected the staged WireGuard config")
	}
	return result, nil
}

func (a wireGuardAdapter) Restart(ctx context.Context) (ServiceResult, error) { return a.service.Restart(ctx) }
func (a wireGuardAdapter) IsActive(ctx context.Context) (ServiceResult, error) { return a.service.IsActive(ctx) }
func (a wireGuardAdapter) IsEnabled(ctx context.Context) (ServiceResult, error) { return a.service.IsEnabled(ctx) }
func (a wireGuardAdapter) ExecuteServiceTask(ctx context.Context, task ConfigTask) (ServiceTaskReport, error) {
	return ExecuteServiceTask(ctx, a.service, task)
}

func (a wireGuardAdapter) CheckHealth(ctx context.Context, _ string) (ListenerHealthResult, error) {
	checkCtx, cancel := context.WithTimeout(ctx, defaultListenerHealthTimeout)
	defer cancel()
	output, err := a.run(checkCtx, a.wgPath, "show", a.interfaceName, "listen-port")
	result := ListenerHealthResult{Address: a.interfaceName}
	if err != nil {
		return result, errors.New("WireGuard interface health check failed")
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || port < 1 || port > 65535 {
		return result, errors.New("WireGuard interface did not report a valid listen port")
	}
	result.Port = port
	return result, nil
}

func defaultTaskValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" { return fallback }
	return strings.TrimSpace(value)
}

type parsedWireGuardConfig struct { Address string; Port int; Peers int }

func parseWireGuardConfig(payload string) (parsedWireGuardConfig, error) {
	section := ""
	interfaceValues := map[string]string{}
	peerValues := map[string]string{}
	parsed := parsedWireGuardConfig{}
	flushPeer := func() error {
		if len(peerValues) == 0 { return nil }
		if err := validateWireGuardKey(peerValues["PublicKey"]); err != nil { return errors.New("WireGuard peer public key is invalid") }
		prefix, err := netip.ParsePrefix(peerValues["AllowedIPs"])
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 { return errors.New("WireGuard peer AllowedIPs must be one IPv4 /32") }
		parsed.Peers++
		peerValues = map[string]string{}
		return nil
	}
	for _, rawLine := range strings.Split(strings.TrimSpace(payload), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") { continue }
		if line == "[Interface]" || line == "[Peer]" {
			if line == "[Peer]" { if err := flushPeer(); err != nil { return parsedWireGuardConfig{}, err } }
			section = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" { return parsedWireGuardConfig{}, errors.New("WireGuard config contains an invalid line") }
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if strings.ContainsAny(value, "\r\n") { return parsedWireGuardConfig{}, errors.New("WireGuard config values must be single-line") }
		if section == "Interface" {
			allowed := map[string]bool{"PrivateKey": true, "Address": true, "ListenPort": true, "SaveConfig": true, "PostUp": true, "PostDown": true}
			if !allowed[key] { return parsedWireGuardConfig{}, fmt.Errorf("WireGuard Interface field %q is not allowed", key) }
			interfaceValues[key] = value
		} else {
			if key != "PublicKey" && key != "AllowedIPs" { return parsedWireGuardConfig{}, fmt.Errorf("WireGuard Peer field %q is not allowed", key) }
			peerValues[key] = value
		}
	}
	if err := flushPeer(); err != nil { return parsedWireGuardConfig{}, err }
	if err := validateWireGuardKey(interfaceValues["PrivateKey"]); err != nil { return parsedWireGuardConfig{}, errors.New("WireGuard server private key is invalid") }
	prefix, err := netip.ParsePrefix(interfaceValues["Address"])
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 { return parsedWireGuardConfig{}, errors.New("WireGuard Interface Address is invalid") }
	port, err := strconv.Atoi(interfaceValues["ListenPort"])
	if err != nil || port < 1 || port > 65535 { return parsedWireGuardConfig{}, errors.New("WireGuard ListenPort is invalid") }
	if interfaceValues["SaveConfig"] != "false" { return parsedWireGuardConfig{}, errors.New("WireGuard SaveConfig must be false") }
	network := prefix.Masked().String()
	postUp := "iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -j ACCEPT; iptables -t nat -A POSTROUTING -s " + network + " -j MASQUERADE"
	postDown := "iptables -D FORWARD -i %i -j ACCEPT; iptables -D FORWARD -o %i -j ACCEPT; iptables -t nat -D POSTROUTING -s " + network + " -j MASQUERADE"
	if interfaceValues["PostUp"] != postUp || interfaceValues["PostDown"] != postDown { return parsedWireGuardConfig{}, errors.New("WireGuard hook commands do not match the fixed RouteGate forwarding policy") }
	parsed.Address, parsed.Port = prefix.String(), port
	return parsed, nil
}

func validateWireGuardKey(value string) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != 32 { return errors.New("invalid WireGuard key") }
	return nil
}
