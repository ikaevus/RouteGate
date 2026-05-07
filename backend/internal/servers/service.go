package servers

import (
	"context"
	"errors"
	"strings"
)

var ErrServerNameRequired = errors.New("server name is required")

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) List(ctx context.Context) ([]Server, error) {
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

func (s *Service) Create(ctx context.Context, request CreateServerRequest) (Server, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.Hostname = strings.TrimSpace(request.Hostname)
	request.PublicIP = strings.TrimSpace(request.PublicIP)
	request.Location = strings.TrimSpace(request.Location)
	request.Provider = strings.TrimSpace(request.Provider)

	if request.Name == "" {
		return Server{}, ErrServerNameRequired
	}

	return s.repository.Create(ctx, request)
}
