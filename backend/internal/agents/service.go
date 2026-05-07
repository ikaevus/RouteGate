package agents

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrAgentNameRequired = errors.New("agent name is required")
	ErrAgentIDRequired   = errors.New("agent id is required")
	ErrAgentNotFound     = errors.New("agent not found")
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) List(ctx context.Context) ([]Agent, error) {
	items, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}

	if len(items) > 0 {
		return items, nil
	}

	if err := s.repository.SeedDemo(ctx); err != nil {
		return nil, err
	}

	return s.repository.List(ctx)
}

func (s *Service) Register(ctx context.Context, request RegisterAgentRequest) (Agent, error) {
	request.ServerID = strings.TrimSpace(request.ServerID)
	request.Name = strings.TrimSpace(request.Name)
	request.Version = strings.TrimSpace(request.Version)
	request.Hostname = strings.TrimSpace(request.Hostname)

	if request.Name == "" {
		return Agent{}, ErrAgentNameRequired
	}

	return s.repository.Register(ctx, request)
}

func (s *Service) Heartbeat(ctx context.Context, request HeartbeatRequest) (time.Time, error) {
	request.AgentID = strings.TrimSpace(request.AgentID)
	request.Version = strings.TrimSpace(request.Version)
	request.Hostname = strings.TrimSpace(request.Hostname)
	request.Status = strings.TrimSpace(request.Status)

	if request.AgentID == "" {
		return time.Time{}, ErrAgentIDRequired
	}

	timestamp, found, err := s.repository.Heartbeat(ctx, request)
	if err != nil {
		return time.Time{}, err
	}

	if !found {
		return time.Time{}, ErrAgentNotFound
	}

	return timestamp, nil
}
