package configs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ikaevus/routegate/backend/internal/traffic"
)

var ErrInvalidRenderedConfig = errors.New("rendered config is invalid")
var ErrConfigVersionNotValidated = errors.New("config version is not validated")
var ErrConfigApplyAgentMissing = errors.New("server has no registered agent")
var ErrConfigApplyUnsafe = errors.New("config version is unsafe to apply")
var ErrConfigHashMismatch = errors.New("config version hash mismatch")

const (
	singBoxVLESSInboundTag = "vless-in"
	singBoxDirectTag       = "direct"
	singBoxBlockTag        = "block"
	defaultVLESSPort      = 443
)

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
	info, err := s.repository.GetServerConfigInfo(ctx, serverID)
	if err != nil {
		return RenderConfigResponse{}, err
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
	config := RenderedConfig{
		SchemaVersion: SchemaVersion,
		Server: ConfigServer{
			ID:        info.ID,
			Name:      info.Name,
			Hostname:  info.Hostname,
			PublicIP:  info.PublicIP,
			PrivateIP: info.PrivateIP,
			Location:  info.Location,
			Provider:  info.Provider,
			Status:    info.Status,
		},
		VPNAccounts: []ConfigVPNAccount{},
		SingBox: SingBoxConfig{
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
		},
		Metadata: ConfigMetadata{
			Source:     "routegate-manager",
			RenderedAt: renderedAt,
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

		config.SingBox.Inbounds = append(config.SingBox.Inbounds, map[string]any{
			"type":        "vless",
			"tag":         singBoxVLESSInboundTag,
			"listen":      "::",
			"listen_port": serverVLESSPort(info),
			"users":       users,
		})
	}

	applyServerRoutingProfile(&config, info.RoutingProfile)

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

		serverOutbound, ok := serverRoutingOutboundForAction(rule.Action)
		if !ok {
			continue
		}
		routeRule := singBoxRouteRule(rule, serverOutbound)
		if len(routeRule) == 0 {
			continue
		}
		if serverOutbound == singBoxBlockTag {
			ensureSingBoxOutbound(config, SingBoxOutbound{Type: "block", Tag: singBoxBlockTag})
		}
		config.SingBox.Route.Rules = append(config.SingBox.Route.Rules, routeRule)
	}
}

func serverRoutingOutboundForAction(action string) (string, bool) {
	switch strings.TrimSpace(action) {
	case routingActionDirect:
		return singBoxDirectTag, true
	case routingActionBlock:
		return singBoxBlockTag, true
	default:
		return "", false
	}
}

func routingOutboundForAction(action string) string {
	switch strings.TrimSpace(action) {
	case routingActionDirect:
		return singBoxDirectTag
	case routingActionBlock:
		return singBoxBlockTag
	case routingActionVPN:
		return routingActionVPN
	default:
		return ""
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
		if !isVPNAccountRenderable(account) {
			continue
		}
		renderable = append(renderable, account)
	}
	return renderable
}

func isVPNAccountRenderable(account VPNAccountConfigInfo) bool {
	if account.Status != "active" {
		return false
	}
	if strings.TrimSpace(account.VLESSUUID) == "" {
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
	if len(config.SingBox.Outbounds) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "singBox.outbounds must contain at least one outbound.")
	}
	if config.SingBox.Route.Final == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "singBox.route.final is required.")
	}
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
	if !validation.Valid {
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

func hashRenderedConfig(config RenderedConfig) (string, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
