package observability

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const diagnosticSyncInterval = time.Second

type DiagnosticWorker struct {
	repository *DiagnosticRepository
	logger     *slog.Logger
	interval   time.Duration
}

func NewDiagnosticWorker(logger *slog.Logger, pool *pgxpool.Pool) *DiagnosticWorker {
	return &DiagnosticWorker{
		repository: NewDiagnosticRepository(pool),
		logger:     logger,
		interval:   diagnosticSyncInterval,
	}
}

func (w *DiagnosticWorker) Run(ctx context.Context) error {
	w.syncSafe(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.syncSafe(ctx)
		}
	}
}

func (w *DiagnosticWorker) syncSafe(ctx context.Context) {
	updated, err := w.repository.SyncSemanticFromAgentJobs(ctx)
	if err != nil {
		if w.logger != nil {
			w.logger.Error("diagnostic run synchronization failed", "error", err)
		}
		return
	}
	if updated > 0 && w.logger != nil {
		w.logger.Debug("diagnostic runs synchronized", "count", updated)
	}
}
