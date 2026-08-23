package updates

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreatePreflight(ctx context.Context, createdByUserID string) (Job, error) {
	return scanJob(r.pool.QueryRow(ctx, `
		INSERT INTO update_jobs (
			operation,
			status,
			stage,
			request_payload,
			created_by_user_id
		)
		VALUES ($1, $2, $3, '{}'::jsonb, NULLIF($4, '')::uuid)
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
	`, OperationPreflight, StatusPending, StagePreflight, createdByUserID))
}

func (r *Repository) MarkRunning(ctx context.Context, id string) (Job, error) {
	return scanJob(r.pool.QueryRow(ctx, `
		UPDATE update_jobs
		SET status = $2,
		    started_at = COALESCE(started_at, now()),
		    updated_at = now()
		WHERE id = $1::uuid
		  AND status = $3
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
	`, id, StatusRunning, StatusPending))
}

func (r *Repository) CompletePreflight(ctx context.Context, id string, result PreflightResult) (Job, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	return scanJob(r.pool.QueryRow(ctx, `
		UPDATE update_jobs
		SET status = $2,
		    result_payload = $3::jsonb,
		    error_code = NULL,
		    updated_at = now(),
		    completed_at = now()
		WHERE id = $1::uuid
		  AND status = $4
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
	`, id, StatusSucceeded, payload, StatusRunning))
}

func (r *Repository) Fail(ctx context.Context, id, errorCode string) (Job, error) {
	return scanJob(r.pool.QueryRow(ctx, `
		UPDATE update_jobs
		SET status = $2,
		    error_code = $3,
		    updated_at = now(),
		    completed_at = now()
		WHERE id = $1::uuid
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
	`, id, StatusFailed, errorCode, StatusPending, StatusRunning))
}

func (r *Repository) Get(ctx context.Context, id string) (Job, error) {
	return scanJob(r.pool.QueryRow(ctx, `
		SELECT
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
		FROM update_jobs
		WHERE id = $1::uuid
	`, id))
}

func (r *Repository) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
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
		FROM update_jobs
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, job)
	}
	return items, rows.Err()
}

func scanJob(row pgx.Row) (Job, error) {
	var job Job
	var requestPayload []byte
	var resultPayload []byte
	if err := row.Scan(
		&job.ID,
		&job.Operation,
		&job.Status,
		&job.Stage,
		&requestPayload,
		&resultPayload,
		&job.ErrorCode,
		&job.CreatedByUserID,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.CompletedAt,
	); err != nil {
		return Job{}, err
	}
	job.RequestPayload = json.RawMessage(requestPayload)
	job.ResultPayload = json.RawMessage(resultPayload)
	return job, nil
}
