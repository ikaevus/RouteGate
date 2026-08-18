package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

// VPNCoreAdapter owns the VPN Core-specific parts of the Agent apply
// lifecycle. Atomic file promotion and rollback remain shared Agent behavior.
type VPNCoreAdapter interface {
	Descriptor() platform.VPNCoreAdapterDescriptor
	Stage(ConfigTask) (StageResult, error)
	Validate(context.Context, string) (ValidationResult, error)
	Restart(context.Context) (ServiceResult, error)
	IsActive(context.Context) (ServiceResult, error)
	IsEnabled(context.Context) (ServiceResult, error)
	ExecuteServiceTask(context.Context, ConfigTask) (ServiceTaskReport, error)
	CheckHealth(context.Context, string) (ListenerHealthResult, error)
}

func SelectVPNCoreAdapter(task ConfigTask, vless, wireGuard VPNCoreAdapter) (VPNCoreAdapter, error) {
	var envelope struct {
		Metadata struct {
			VPNCore struct {
				Core string `json:"core"`
				Protocol string `json:"protocol"`
				Transport string `json:"transport"`
				Security string `json:"security"`
			} `json:"vpnCore"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(task.RenderedConfig, &envelope); err != nil {
		return nil, errors.New("rendered config must be valid JSON")
	}
	descriptor := envelope.Metadata.VPNCore
	if strings.TrimSpace(descriptor.Core) == "" {
		if vless == nil { return nil, errors.New("VLESS VPN Core adapter is unavailable") }
		return vless, nil
	}
	if descriptor.Core == platform.VPNCoreSingBox && descriptor.Protocol == platform.VPNProtocolVLESS && descriptor.Transport == platform.VPNTransportTCP && (descriptor.Security == platform.VPNSecurityNone || descriptor.Security == platform.VPNSecurityReality) {
		if vless == nil { return nil, errors.New("VLESS VPN Core adapter is unavailable") }
		return vless, nil
	}
	if descriptor.Core == platform.VPNCoreWireGuard && descriptor.Protocol == platform.VPNProtocolWireGuard && descriptor.Transport == platform.VPNTransportUDP && descriptor.Security == platform.VPNSecurityWireGuard {
		if wireGuard == nil { return nil, errors.New("WireGuard VPN Core adapter is unavailable") }
		return wireGuard, nil
	}
	return nil, errors.New("rendered config selects an unsupported VPN Core adapter")
}

type singBoxVLESSAdapter struct {
	stager    Stager
	validator Validator
	service   ServiceController
}

var _ VPNCoreAdapter = singBoxVLESSAdapter{}

func NewSingBoxVLESSAdapter(stagingDir, binary, service string) VPNCoreAdapter {
	return singBoxVLESSAdapter{
		stager:    NewStager(stagingDir),
		validator: NewValidator(binary),
		service:   NewServiceController(service),
	}
}

func (singBoxVLESSAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.ManagedVPNCoreAdapters()[0]
}

func (a singBoxVLESSAdapter) Stage(task ConfigTask) (StageResult, error) {
	return a.stager.Stage(task)
}

func (a singBoxVLESSAdapter) Validate(ctx context.Context, configPath string) (ValidationResult, error) {
	return a.validator.Check(ctx, configPath)
}

func (a singBoxVLESSAdapter) Restart(ctx context.Context) (ServiceResult, error) {
	return a.service.Restart(ctx)
}

func (a singBoxVLESSAdapter) IsActive(ctx context.Context) (ServiceResult, error) {
	return a.service.IsActive(ctx)
}

func (a singBoxVLESSAdapter) IsEnabled(ctx context.Context) (ServiceResult, error) {
	return a.service.IsEnabled(ctx)
}

func (a singBoxVLESSAdapter) ExecuteServiceTask(ctx context.Context, task ConfigTask) (ServiceTaskReport, error) {
	return ExecuteServiceTask(ctx, a.service, task)
}

func (singBoxVLESSAdapter) CheckHealth(ctx context.Context, configPath string) (ListenerHealthResult, error) {
	return CheckVLESSListener(ctx, configPath)
}
