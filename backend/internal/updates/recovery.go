package updates

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

const (
	ErrorCodePreflightInterrupted = "preflight_interrupted"
	ErrorCodeDiscoveryInterrupted = "discovery_interrupted"
	ErrorCodeStageInterrupted     = "stage_interrupted"
)

type interruptedJobRepository interface {
	RecoverInterruptedJobs(context.Context) ([]Job, error)
}

type interruptedStageCleaner interface {
	Cleanup(string) error
}

func RecoverInterruptedJobs(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) error {
	return recoverInterruptedJobsWithCleaner(ctx, logger, NewRepository(pool), audit.NewRecorder(logger, pool), newReleaseArtifactStager())
}

func recoverInterruptedJobs(ctx context.Context, logger *slog.Logger, repo interruptedJobRepository, recorder auditRecorder) error {
	return recoverInterruptedJobsWithCleaner(ctx, logger, repo, recorder, nil)
}

func recoverInterruptedJobsWithCleaner(ctx context.Context, logger *slog.Logger, repo interruptedJobRepository, recorder auditRecorder, cleaner interruptedStageCleaner) error {
	jobs, err := repo.RecoverInterruptedJobs(ctx)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if job.Operation == OperationStage && cleaner != nil {
			if err := cleaner.Cleanup(job.ID); err != nil {
				return fmt.Errorf("clean interrupted update stage %s: %w", job.ID, err)
			}
		}

		action := interruptedAuditAction(job.Operation)
		if action == "" {
			continue
		}
		if recorder != nil {
			recorder.RecordSafe(ctx, audit.EventInput{
				ActorType:    audit.ActorTypeSystem,
				Action:       action,
				ResourceType: "update_job",
				ResourceID:   job.ID,
				Result:       audit.ResultFailure,
				Metadata: map[string]any{
					"operation":  job.Operation,
					"stage":      job.Stage,
					"status":     job.Status,
					"error_code": job.ErrorCode,
				},
			})
		}
	}

	if len(jobs) > 0 && logger != nil {
		logger.Warn("recovered interrupted update jobs", "count", len(jobs))
	}
	return nil
}

func interruptedAuditAction(operation string) string {
	switch operation {
	case OperationPreflight:
		return "update.preflight.interrupted"
	case OperationDiscovery:
		return "update.discovery.interrupted"
	case OperationStage:
		return "update.stage.interrupted"
	default:
		return ""
	}
}

func (r *Repository) RecoverInterruptedJobs(ctx context.Context) ([]Job, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE update_jobs
		SET status = $1,
		    error_code = CASE operation
		        WHEN $2 THEN $3
		        WHEN $4 THEN $5
		        WHEN $6 THEN $7
		    END,
		    updated_at = now(),
		    completed_at = now()
		WHERE operation IN ($2, $4, $6)
		  AND status IN ($8, $9)
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
	`, StatusFailed,
		OperationPreflight, ErrorCodePreflightInterrupted,
		OperationDiscovery, ErrorCodeDiscoveryInterrupted,
		OperationStage, ErrorCodeStageInterrupted,
		StatusPending, StatusRunning,
	)
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
