package configs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

var ErrInvalidRenderedConfig = errors.New("rendered config is invalid")
var ErrConfigVersionNotValidated = errors.New("config version is not validated")
var ErrConfigApplyAgentMissing = errors.New("server has no registered agent")
var ErrConfigApplyUnsafe = errors.New("config version is unsafe to apply")
var ErrConfigHashMismatch = errors.New("config version hash mismatch")
var ErrNodeRoleNoVPN = errors.New("node deployment role does not host the VPN plane")

const (
	routingActionDirect = "direct"
	routingActionVPN    = "vpn"
	routingActionBlock  = "block"
)

type configRepository interface {
	GetServerConfigInfo(context.Context, string) (ServerConfigInfo, error)
	CreateConfigVersion(context.Context, CreateConfigVersionInput) (ConfigVersion, error)
	ListConfigVersions(context.Context, string) ([]ConfigVersion, error)
	GetConfigVersion(context.Context, string, string) (ConfigVersion, error)
	MarkConfigVersionValidated(context.Context, string, string) (ConfigVersion, error)
	CreateConfigApplyJob(context.Context, CreateConfigApplyJobInput) (ConfigApplyJob, error)
	ListConfigApplyJobs(context.Context, string) ([]ConfigApplyJob, error)
	GetConfigApplyJob(context.Context, string, string) (ConfigApplyJob, error)
}

type Service struct {
	repository configRepository
	now        func() time.Time
}

func NewService(repository configRepository) *Service {
	return &Service{
		repository: repository,
		now:        time.Now,
	}
}

func (s *Service) Render(ctx context.Context, serverID string) (RenderConfigResponse, error) {
	resolvedProtocols, err := resolveServerAccountProtocols(ctx, s.repository, serverID)
	if err != nil {
		return RenderConfigResponse{}, err
	}
	info, err := s.repository.GetServerConfigInfo(ctx, serverID)
	if err != nil {
		return RenderConfigResponse{}, err
	}
	applyResolvedAccountProtocols(&info, resolvedProtocols)
	if !platform.EffectiveDeploymentRole(info.DeploymentRole).HostsVPNPlane() {
		return RenderConfigResponse{}, ErrNodeRoleNoVPN
	}

	rendered := buildRenderedConfig(info, s.now().UTC())
	validation := ValidateRenderedConfig(rendered)
	status := StatusRendered
	if validation.Valid {
		status = StatusValidated
	} else {
		status = StatusValidationFailed
	}

	hash, err := hashRenderedConfig(rendered)
	if err != nil {
		return RenderConfigResponse{}, err
	}

	version, err := s.repository.CreateConfigVersion(ctx, CreateConfigVersionInput{
		ServerID:       serverID,
		Status:         status,
		ConfigHash:     hash,
		RenderedConfig: rendered,
	})
	if err != nil {
		return RenderConfigResponse{}, err
	}

	return RenderConfigResponse{ConfigVersion: version, ValidationResult: validation}, nil
}

func (s *Service) List(ctx context.Context, serverID string) ([]ConfigVersion, error) {
	return s.repository.ListConfigVersions(ctx, serverID)
}

func (s *Service) Get(ctx context.Context, serverID, versionID string) (ConfigVersion, error) {
	return s.repository.GetConfigVersion(ctx, serverID, versionID)
}

func (s *Service) Validate(ctx context.Context, serverID, versionID string) (ValidateConfigResponse, error) {
	version, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return ValidateConfigResponse{}, err
	}

	var rendered RenderedConfig
	if err := json.Unmarshal(version.RenderedConfig, &rendered); err != nil {
		return ValidateConfigResponse{}, err
	}

	validation := ValidateRenderedConfig(rendered)
	if !validation.Valid {
		return ValidateConfigResponse{ConfigVersion: version, ValidationResult: validation}, ErrInvalidRenderedConfig
	}

	version, err = s.repository.MarkConfigVersionValidated(ctx, serverID, versionID)
	if err != nil {
		return ValidateConfigResponse{}, err
	}
	return ValidateConfigResponse{ConfigVersion: version, ValidationResult: validation}, nil
}

func (s *Service) Apply(ctx context.Context, serverID, versionID string, request ApplyConfigRequest) (ApplyConfigResponse, error) {
	version, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	if version.Status != StatusValidated {
		return ApplyConfigResponse{}, ErrConfigVersionNotValidated
	}
	if err := ensureConfigVersionSafeForApply(version); err != nil {
		return ApplyConfigResponse{}, err
	}

	info, err := s.repository.GetServerConfigInfo(ctx, serverID)
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	if !platform.EffectiveDeploymentRole(info.DeploymentRole).HostsVPNPlane() {
		return ApplyConfigResponse{}, ErrNodeRoleNoVPN
	}
	if info.Agent == nil {
		return ApplyConfigResponse{}, ErrConfigApplyAgentMissing
	}

	job, err := s.repository.CreateConfigApplyJob(ctx, CreateConfigApplyJobInput{
		ServerID:        serverID,
		AgentID:         info.Agent.ID,
		ConfigVersionID: version.ID,
		Action:          ApplyJobActionApply,
		RequestPayload: map[string]any{
			"comment":     strings.TrimSpace(request.Comment),
			"config_hash": version.ConfigHash,
		},
	})
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	return ApplyConfigResponse{Job: job}, nil
}

func (s *Service) ListApplyJobs(ctx context.Context, serverID string) ([]ConfigApplyJob, error) {
	return s.repository.ListConfigApplyJobs(ctx, serverID)
}

func (s *Service) GetApplyJob(ctx context.Context, serverID, jobID string) (ConfigApplyJob, error) {
	return s.repository.GetConfigApplyJob(ctx, serverID, jobID)
}

func buildRenderedConfig(info ServerConfigInfo, renderedAt time.Time) RenderedConfig {
	realityEnabled := realityRequested(info)
	legacyAdapter := selectedVPNCoreAdapter(info)
	config := RenderedConfig{
		SchemaVersion: SchemaVersion,
		Server: ConfigServer{
			ID:             info.ID,
			Name:           info.Name,
			DeploymentRole: string(platform.EffectiveDeploymentRole(info.DeploymentRole)),
			Hostname:       info.Hostname,
			PublicIP:       info.PublicIP,
			PrivateIP:      info.PrivateIP,
			Location:       info.Location,
			Provider:       info.Provider,
			Status:         info.Status,
		},
		VPNAccounts: []ConfigVPNAccount{},
		Metadata: ConfigMetadata{
			Source:         "routegate-manager",
			RenderedAt:     renderedAt,
			RealityEnabled: realityEnabled,
			VPNCore:        configVPNCoreFromAdapter(legacyAdapter, realityEnabled),
		},
	}

	if info.Agent != nil {
		config.Agent = &ConfigAgent{
			ID:           info.Agent.ID,
			Hostname:     info.Agent.Hostname,
			OS:           info.Agent.OS,
			Arch:         info.Agent.Arch,
			AgentVersion: info.Agent.AgentVersion,
			Status:       info.Agent.Status,
			Capabilities: info.Agent.Capabilities,
		}
		if config.Agent.Capabilities == nil {
			config.Agent.Capabilities = map[string]any{}
		}
	}

	applyServerRoutingProfile(&config, info.RoutingProfile)
	renderSelectedVPNCoreAdapters(&config, info)

	return config
}

func applyServerRoutingProfile(config *RenderedConfig, profile *RoutingProfileConfigInfo) {
	if profile == nil {
		return
	}

	config.RoutingProfile = &ConfigRoutingProfile{
		ID:          profile.ID,
		Name:        profile.Name,
		Description: strings.TrimSpace(profile.Description),
		IsDefault:   profile.IsDefault,
		Rules:       []ConfigRoutingProfileRule{},
	}

	for _, rule := range profile.Rules {
		outbound := routingOutboundForAction(rule.Action)
		config.RoutingProfile.Rules = append(config.RoutingProfile.Rules, ConfigRoutingProfileRule{
			ID:             rule.ID,
			Name:           rule.Name,
			Priority:       rule.Priority,
			Action:         rule.Action,
			Outbound:       outbound,
			Domains:        cleanStrings(rule.Domains),
			DomainSuffixes: cleanStrings(rule.DomainSuffixes),
			DomainKeywords: cleanStrings(rule.DomainKeywords),
			IPCIDRs:        cleanStrings(rule.IPCIDRs),
			GeoSites:       cleanStrings(rule.GeoSites),
			GeoIPs:         cleanStrings(rule.GeoIPs),
		})
	}
}

func routingOutboundForAction(action string) string {
	switch strings.TrimSpace(action) {
	case routingActionDirect:
		return routingActionDirect
	case routingActionBlock:
		return routingActionBlock
	case routingActionVPN:
		return routingActionVPN
	default:
		return ""
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

func ValidateRenderedConfig(config RenderedConfig) ValidationResult {
	result := ValidationResult{Valid: true, Errors: []string{}, Warnings: []string{}}
	if config.SchemaVersion != SchemaVersion {
		result.Valid = false
		result.Errors = append(result.Errors, "schemaVersion must be "+SchemaVersion+".")
	}
	if config.Server.ID == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "server.id is required.")
	}
	if config.Server.Name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "server.name is required.")
	}
	if role := strings.TrimSpace(config.Server.DeploymentRole); role != "" {
		parsed, err := platform.ParseDeploymentRole(role, platform.DeploymentRoleVPN)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error()+".")
		} else if !parsed.HostsVPNPlane() {
			result.Valid = false
			result.Errors = append(result.Errors, "server.deploymentRole does not host the VPN plane.")
		}
	}
	validateConfiguredVPNCoreAdapters(config, &result)
	if config.Agent == nil {
		result.Warnings = append(result.Warnings, "No registered agent is attached to this server yet; the config can be rendered but cannot be applied.")
	}
	return result
}

func ensureConfigVersionSafeForApply(version ConfigVersion) error {
	if len(version.RenderedConfig) == 0 || !json.Valid(version.RenderedConfig) {
		return ErrConfigApplyUnsafe
	}

	var rendered RenderedConfig
	if err := json.Unmarshal(version.RenderedConfig, &rendered); err != nil {
		return ErrConfigApplyUnsafe
	}
	validation := ValidateRenderedConfig(rendered)
	if !validation.Valid || !vpnServiceReady(rendered) {
		return ErrConfigApplyUnsafe
	}

	hash, err := hashRenderedConfig(rendered)
	if err != nil {
		return err
	}
	if strings.TrimSpace(version.ConfigHash) == "" || !strings.EqualFold(hash, strings.TrimSpace(version.ConfigHash)) {
		return ErrConfigHashMismatch
	}
	return nil
}

func vpnServiceReady(config RenderedConfig) bool {
	return configuredVPNServicesReady(config)
}

func selectedAdapterSecurity(descriptor platform.VPNCoreAdapterDescriptor, realityEnabled bool) string {
	if descriptor.Protocol == platform.VPNProtocolVLESS {
		if realityEnabled {
			return platform.VPNSecurityReality
		}
		return platform.VPNSecurityNone
	}
	if len(descriptor.SecurityModes) > 0 {
		return descriptor.SecurityModes[0]
	}
	return ""
}

func hashRenderedConfig(config RenderedConfig) (string, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
