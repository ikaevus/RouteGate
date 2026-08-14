package configs

import (
	"context"
	"errors"
)

var ErrConfigHistoryRepositoryUnavailable = errors.New("config history repository is unavailable")

type configHistoryRepository interface {
	ListConfigApplyJobsPage(context.Context, string, int, int) ([]ConfigApplyJob, int, error)
	DeleteTerminalConfigApplyJobs(context.Context, string) (int64, error)
}

func (s *Service) ListApplyJobsPage(ctx context.Context, serverID string, limit, offset int) ([]ConfigApplyJob, int, error) {
	repository, ok := s.repository.(configHistoryRepository)
	if !ok {
		return nil, 0, ErrConfigHistoryRepositoryUnavailable
	}
	return repository.ListConfigApplyJobsPage(ctx, serverID, limit, offset)
}

func (s *Service) ClearCompletedApplyHistory(ctx context.Context, serverID string) (int64, error) {
	repository, ok := s.repository.(configHistoryRepository)
	if !ok {
		return 0, ErrConfigHistoryRepositoryUnavailable
	}
	return repository.DeleteTerminalConfigApplyJobs(ctx, serverID)
}
