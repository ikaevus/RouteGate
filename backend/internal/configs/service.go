package configs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrInvalidRenderedConfig = errors.New("rendered config is invalid")
var ErrConfigVersionNotValidated = errors.New("config version is not validated")
var ErrConfigApplyAgentMissing = errors.New("server has no registered agent")

type configRepository interface {
	GetServerConfigInfo(context.Context, string) (ServerConfigInfo, error)
	CreateConfigVersion(context.Context, CreateConfigVersionInput) (ConfigVersion, error)
	ListConfigVersions(context.Context, string) ([]ConfigVersion, error)
	GetConfigVersion(context.Context, string, string) (ConfigVersion, error)
	MarkConfigVersionValidated(context.Context, string, string) (ConfigVersion, error)
	CreateConfigApplyJob(context.Context, CreateConfigApplyJobInput) (ConfigApplyJob, error)
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
			"comment": request.Comment,
		},
	})
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	return ApplyConfigResponse{Job: job}, nil
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
		SingBox: SingBoxConfig{
			Log:      SingBoxLog{Level: "info"},
			Inbounds: []map[string]any{},
			Outbounds: []SingBoxOutbound{{
				Type: "direct",
				Tag:  "direct",
			}},
			Route: SingBoxRoute{
				Rules: []map[string]any{},
				Final: "direct",
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

	return config
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

func hashRenderedConfig(config RenderedConfig) (string, error) {
	payload, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
