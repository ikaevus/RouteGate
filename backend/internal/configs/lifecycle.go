package configs

import (
	"context"
	"errors"
	"strings"
)

var ErrConfigVersionInUse = errors.New("config version is immutable because it has deployment history")
var ErrConfigVersionNeverApplied = errors.New("config version has never been applied")

type configVersionDeletionRepository interface {
	DeleteUnusedConfigVersion(context.Context, string, string) (bool, error)
}

func (s *Service) DeleteUnused(ctx context.Context, serverID, versionID string) error {
	repository, ok := s.repository.(configVersionDeletionRepository)
	if !ok {
		return errors.New("config repository does not support version deletion")
	}

	deleted, err := repository.DeleteUnusedConfigVersion(ctx, serverID, versionID)
	if err != nil {
		return err
	}
	if deleted {
		return nil
	}

	if _, err := s.repository.GetConfigVersion(ctx, serverID, versionID); err != nil {
		return err
	}
	return ErrConfigVersionInUse
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
