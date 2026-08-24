package updates

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CreatePreflight allocates the job ID before issuing the INSERT. If the SQL
// result is ambiguous, the returned Job still carries that ID so the caller
// can immediately reconcile a row that may already have committed.
func (r *Repository) CreatePreflight(ctx context.Context, createdByUserID string) (Job, error) {
	return r.createJob(ctx, OperationPreflight, StagePreflight, createdByUserID)
}

// CreateDiscovery follows the same preallocated-ID durability contract as
// CreatePreflight so an ambiguous INSERT can be reconciled immediately.
func (r *Repository) CreateDiscovery(ctx context.Context, createdByUserID string) (Job, error) {
	return r.createJob(ctx, OperationDiscovery, StageDiscovery, createdByUserID)
}

func (r *Repository) createJob(ctx context.Context, operation, stage, createdByUserID string) (Job, error) {
	id, err := newUpdateJobID()
	if err != nil {
		return Job{}, err
	}
	pending := Job{
		ID:              id,
		Operation:       operation,
		Status:          StatusPending,
		Stage:           stage,
		RequestPayload:  json.RawMessage(`{}`),
		ResultPayload:   json.RawMessage(`{}`),
		CreatedByUserID: createdByUserID,
	}

	job, err := scanJob(r.pool.QueryRow(ctx, `
		INSERT INTO update_jobs (
			id,
			operation,
			status,
			stage,
			request_payload,
			created_by_user_id
		)
		VALUES ($1::uuid, $2, $3, $4, '{}'::jsonb, NULLIF($5, '')::uuid)
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
	`, id, operation, StatusPending, stage, createdByUserID))
	if err != nil {
		return pending, err
	}
	return job, nil
}

func newUpdateJobID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
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
	return r.completeJob(ctx, id, OperationPreflight, payload)
}

func (r *Repository) CompleteDiscovery(ctx context.Context, id string, result DiscoveryResult) (Job, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	return r.completeJob(ctx, id, OperationDiscovery, payload)
}

func (r *Repository) completeJob(ctx context.Context, id, operation string, payload []byte) (Job, error) {
	return scanJob(r.pool.QueryRow(ctx, `
		UPDATE update_jobs
		SET status = $2,
		    result_payload = $3::jsonb,
		    error_code = NULL,
		    updated_at = now(),
		    completed_at = now()
		WHERE id = $1::uuid
		  AND operation = $4
		  AND status = $5
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
	`, id, StatusSucceeded, payload, operation, StatusRunning))
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
