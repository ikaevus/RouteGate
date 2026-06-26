package audit

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type eventRepository interface {
	Create(context.Context, EventInput) (Event, error)
}

type Recorder struct {
	logger *slog.Logger
	repo   eventRepository
}

func NewRecorder(logger *slog.Logger, pool *pgxpool.Pool) *Recorder {
	return &Recorder{
		logger: logger,
		repo:   NewRepository(pool),
	}
}

func NewRecorderWithRepository(logger *slog.Logger, repo eventRepository) *Recorder {
	return &Recorder{logger: logger, repo: repo}
}

func (r *Recorder) Record(ctx context.Context, input EventInput) (Event, error) {
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	return r.repo.Create(ctx, input)
}

func (r *Recorder) RecordSafe(ctx context.Context, input EventInput) {
	if r == nil || r.repo == nil {
		return
	}
	if _, err := r.Record(ctx, input); err != nil && r.logger != nil {
		r.logger.Warn(
			"audit event recording failed",
			"action", input.Action,
			"resource_type", input.ResourceType,
			"error", err,
		)
	}
}
