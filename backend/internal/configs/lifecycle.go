package configs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/platform"
)

var ErrConfigVersionCurrent = errors.New("current config version cannot be deleted")
var ErrConfigVersionPinned = errors.New("pinned config version cannot be deleted")
var ErrConfigVersionDeploymentActive = errors.New("config version has an active deployment")
var ErrConfigVersionNeverApplied = errors.New("config version has never been applied")

type configVersionLifecycleRepository interface {
	DeleteConfigVersion(context.Context, string, string) (bool, error)
	HasActiveConfigApplyJob(context.Context, string, string) (bool, error)
	SetConfigVersionPinned(context.Context, string, string, bool) (ConfigVersion, error)
	GetCurrentConfigVersionID(context.Context, string) (string, error)
}

func (s *Service) CurrentVersionID(ctx context.Context, serverID string) (string, error) {
	repository, ok := s.repository.(interface {
		GetCurrentConfigVersionID(context.Context, string) (string, error)
	})
	if !ok {
		return "", errors.New("config repository does not support current version state")
	}
	return repository.GetCurrentConfigVersionID(ctx, serverID)
}

func (s *Service) DeleteVersion(ctx context.Context, serverID, versionID string) error {
	repository, ok := s.repository.(configVersionLifecycleRepository)
	if !ok {
		return errors.New("config repository does not support version lifecycle management")
	}

	version, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	currentID, err := repository.GetCurrentConfigVersionID(ctx, serverID)
	if err != nil {
		return err
	}
	if currentID == version.ID {
		return ErrConfigVersionCurrent
	}
	if version.Pinned {
		return ErrConfigVersionPinned
	}
	active, err := repository.HasActiveConfigApplyJob(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	if active {
		return ErrConfigVersionDeploymentActive
	}

	deleted, err := repository.DeleteConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrConfigVersionDeploymentActive
	}
	return nil
}

func (s *Service) SetVersionPinned(ctx context.Context, serverID, versionID string, pinned bool) (ConfigVersion, error) {
	repository, ok := s.repository.(configVersionLifecycleRepository)
	if !ok {
		return ConfigVersion{}, errors.New("config repository does not support version lifecycle management")
	}
	return repository.SetConfigVersionPinned(ctx, serverID, versionID, pinned)
}

func (s *Service) Reapply(ctx context.Context, serverID, versionID string, request ApplyConfigRequest) (ApplyConfigResponse, error) {
	version, err := s.repository.GetConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	if version.AppliedAt == nil {
		return ApplyConfigResponse{}, ErrConfigVersionNeverApplied
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
			"redeploy":    true,
		},
	})
	if err != nil {
		return ApplyConfigResponse{}, err
	}
	return ApplyConfigResponse{Job: job}, nil
}

func renderedConfigsEquivalentForVersioning(left, right []byte) (bool, error) {
	leftNormalized, err := normalizeRenderedConfigForVersioning(left)
	if err != nil {
		return false, err
	}
	rightNormalized, err := normalizeRenderedConfigForVersioning(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftNormalized, rightNormalized), nil
}

func normalizeRenderedConfigForVersioning(payload []byte) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		return nil, err
	}
	if metadata, ok := config["metadata"].(map[string]any); ok {
		delete(metadata, "renderedAt")
	}
	return json.Marshal(config)
}
