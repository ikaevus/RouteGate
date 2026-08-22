package tasks

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

const mtprotoValidationTimeout = 10 * time.Second
const mtprotoFrontingDomain = "www.cloudflare.com"

type mtprotoAdapter struct {
	stagingDir string
	binary     string
	service    ServiceController
	run        commandRunner
}

type mtprotoServerConfig struct {
	Secret string
	Port   int
}

var _ VPNCoreAdapter = mtprotoAdapter{}

func NewMTProtoAdapter(stagingDir, binary, serviceName string) VPNCoreAdapter {
	return mtprotoAdapter{
		stagingDir: strings.TrimSpace(stagingDir),
		binary:     defaultTaskValue(binary, "mtg"),
		service:    NewServiceController(defaultTaskValue(serviceName, "routegate-mtproto")),
		run:        runCommand,
	}
}

func (mtprotoAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.ManagedVPNCoreAdapters()[4]
}

func (a mtprotoAdapter) Stage(task ConfigTask) (StageResult, error) {
	if task.EffectiveKind() != TaskKindConfigApply || task.ID == "" || task.ConfigVersionID == "" {
		return StageResult{}, errors.New("valid MTProto config apply task identifiers are required")
	}
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
		MTProto       string `json:"mtproto"`
	}
	if err := json.Unmarshal(task.RenderedConfig, &envelope); err != nil {
		return StageResult{}, fmt.Errorf("decode rendered config envelope: %w", err)
	}
	if envelope.SchemaVersion != "routegate.config.v1" || strings.TrimSpace(envelope.MTProto) == "" {
		return StageResult{}, errors.New("rendered config envelope must contain MTProto config")
	}
	if _, err := parseMTProtoConfig(envelope.MTProto); err != nil {
		return StageResult{}, err
	}
	if err := os.MkdirAll(a.stagingDir, 0o750); err != nil {
		return StageResult{}, fmt.Errorf("create MTProto staging dir: %w", err)
	}
	path := filepath.Join(a.stagingDir, task.ConfigVersionID+".toml")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(strings.TrimSpace(envelope.MTProto)+"\n"), 0o600); err != nil {
		return StageResult{}, fmt.Errorf("write staged MTProto config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return StageResult{}, fmt.Errorf("commit staged MTProto config: %w", err)
	}
	return StageResult{StagedPath: path, ConfigHash: task.ConfigHash, ConfigVersionID: task.ConfigVersionID}, nil
}

func (a mtprotoAdapter) Validate(ctx context.Context, configPath string) (ValidationResult, error) {
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return ValidationResult{}, fmt.Errorf("read staged MTProto config: %w", err)
	}
	if _, err := parseMTProtoConfig(string(payload)); err != nil {
		return ValidationResult{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, mtprotoValidationTimeout)
	defer cancel()
	// mtg v2 exposes version reporting through -v/--version; "mtg version"
	// is not a valid subcommand and caused healthy installations to fail
	// RouteGate's pre-deploy validation.
	output, err := a.run(checkCtx, a.binary, "--version")
	result := ValidationResult{Command: a.binary + " --version", Output: strings.TrimSpace(string(output))}
	if err != nil {
		if checkCtx.Err() != nil {
			return result, fmt.Errorf("MTProto binary validation timed out: %w", checkCtx.Err())
		}
		return result, errors.New("mtg binary is unavailable or failed its version check")
	}
	return result, nil
}

func (a mtprotoAdapter) Restart(ctx context.Context) (ServiceResult, error) { return a.service.Restart(ctx) }
func (a mtprotoAdapter) IsActive(ctx context.Context) (ServiceResult, error)  { return a.service.IsActive(ctx) }
func (a mtprotoAdapter) IsEnabled(ctx context.Context) (ServiceResult, error) { return a.service.IsEnabled(ctx) }
func (a mtprotoAdapter) ExecuteServiceTask(ctx context.Context, task ConfigTask) (ServiceTaskReport, error) {
	return ExecuteServiceTask(ctx, a.service, task)
}

func (a mtprotoAdapter) CheckHealth(ctx context.Context, configPath string) (ListenerHealthResult, error) {
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return ListenerHealthResult{}, errors.New("read active MTProto config failed")
	}
	config, err := parseMTProtoConfig(string(payload))
	if err != nil {
		return ListenerHealthResult{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, defaultListenerHealthTimeout)
	defer cancel()
	return checkTCPListenerPort(checkCtx, config.Port, "MTProto", defaultListenerHealthTimeout, defaultListenerHealthRetryInterval)
}

func parseMTProtoConfig(payload string) (mtprotoServerConfig, error) {
	config := mtprotoServerConfig{}
	values := map[string]string{}
	allowed := map[string]bool{"debug": true, "secret": true, "bind-to": true, "concurrency": true, "prefer-ip": true, "auto-update": true}
	for _, rawLine := range strings.Split(strings.TrimSpace(payload), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !allowed[key] {
			return config, errors.New("MTProto config must use the fixed RouteGate TOML grammar")
		}
		if _, duplicate := values[key]; duplicate {
			return config, fmt.Errorf("MTProto field %q is duplicated", key)
		}
		values[key] = value
	}
	if len(values) != 6 || values["debug"] != "false" || values["concurrency"] != "8192" ||
		values["prefer-ip"] != "\"prefer-ipv4\"" || values["auto-update"] != "false" {
		return config, errors.New("MTProto config must match the fixed RouteGate runtime policy")
	}
	secret, err := strconv.Unquote(values["secret"])
	if err != nil || !validMTProtoSecret(secret) {
		return config, errors.New("MTProto FakeTLS secret is invalid")
	}
	bindTo, err := strconv.Unquote(values["bind-to"])
	if err != nil || !strings.HasPrefix(bindTo, "0.0.0.0:") {
		return config, errors.New("MTProto bind-to must listen on one fixed TCP port")
	}
	port, err := strconv.Atoi(strings.TrimPrefix(bindTo, "0.0.0.0:"))
	if err != nil || port < 1 || port > 65535 || bindTo != "0.0.0.0:"+strconv.Itoa(port) {
		return config, errors.New("MTProto bind-to port must be between 1 and 65535")
	}
	config.Secret, config.Port = strings.ToLower(secret), port
	return config, nil
}

func validMTProtoSecret(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	return len(value) == 70 && strings.HasPrefix(value, "ee") && err == nil && len(decoded) == 35 && string(decoded[17:]) == mtprotoFrontingDomain
}
