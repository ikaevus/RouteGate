package updates

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

const ErrorCodePreflightInterrupted = "preflight_interrupted"

type interruptedJobRepository interface {
	RecoverInterruptedPreflights(context.Context) ([]Job, error)
}

func RecoverInterruptedJobs(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) error {
	return recoverInterruptedJobs(ctx, logger, NewRepository(pool), audit.NewRecorder(logger, pool))
}

func recoverInterruptedJobs(ctx context.Context, logger *slog.Logger, repo interruptedJobRepository, recorder auditRecorder) error {
	jobs, err := repo.RecoverInterruptedPreflights(ctx)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if recorder != nil {
			recorder.RecordSafe(ctx, audit.EventInput{
				ActorType:    audit.ActorTypeSystem,
				Action:       "update.preflight.interrupted",
				ResourceType: "update_job",
				ResourceID:   job.ID,
				Result:       audit.ResultFailure,
				Metadata: map[string]any{
					"operation":  job.Operation,
					"stage":      job.Stage,
					"status":     job.Status,
					"error_code": ErrorCodePreflightInterrupted,
				},
			})
		}
	}

	if len(jobs) > 0 && logger != nil {
		logger.Warn("recovered interrupted update preflight jobs", "count", len(jobs))
	}
	return nil
}

func (r *Repository) RecoverInterruptedPreflights(ctx context.Context) ([]Job, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE update_jobs
		SET status = $1,
		    error_code = $2,
		    updated_at = now(),
		    completed_at = now()
		WHERE operation = $3
		  AND status IN ($4, $5)
		RETURNING
			id::text,
			operation,
			status,
			stage,
			request_payload,
			result_payload,
			COALESCE(error_code, ''),
			COALESCE(created_by_user_id::text, ''),
			created_at,
			updated_at,
			started_at,
			completed_at
	`, StatusFailed, ErrorCodePreflightInterrupted, OperationPreflight, StatusPending, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}
