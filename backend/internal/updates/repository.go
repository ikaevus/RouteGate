package updates

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxRetainedStageJobs  = 2
	maxReservedStageBytes = int64(maxRetainedStageJobs) * (maxReleaseBundleBytes + maxAttestationBundleBytes + 3*maxSmallReleaseAssetBytes)
	stageRetentionEvicted = "evicted"
)

var ErrStageCapacityExceeded = errors.New("retained stage job capacity exceeded")

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
	return r.createJob(ctx, OperationPreflight, StagePreflight, createdByUserID, json.RawMessage(`{}`))
}

// CreateDiscovery follows the same preallocated-ID durability contract as
// CreatePreflight so an ambiguous INSERT can be reconciled immediately.
func (r *Repository) CreateDiscovery(ctx context.Context, createdByUserID string) (Job, error) {
	return r.createJob(ctx, OperationDiscovery, StageDiscovery, createdByUserID, json.RawMessage(`{}`))
}

// CreateStage preserves the bounded admission contract for callers that do not
// own a staging cleanup boundary. Production staging uses
// CreateStageWithCleanup so successful retained candidates can be evicted
// deterministically when the two-candidate capacity is full.
func (r *Repository) CreateStage(ctx context.Context, createdByUserID, discoveryJobID string) (Job, bool, error) {
	return r.createStage(ctx, createdByUserID, discoveryJobID, nil)
}

// CreateStageWithCleanup serializes duplicate detection, retention eviction and
// job insertion under one PostgreSQL advisory transaction lock. The cleanup
// callback receives only a durable RouteGate stage job ID; the production
// callback maps that ID to the fixed Manager-owned staging root.
//
// A succeeded candidate is marked evicted only after its bytes have been
// removed successfully. If cleanup or the database update fails, the
// transaction rolls back and capacity remains conservatively occupied. The
// historical update_jobs row is never deleted.
func (r *Repository) CreateStageWithCleanup(ctx context.Context, createdByUserID, discoveryJobID string, cleanup func(string) error) (Job, bool, error) {
	return r.createStage(ctx, createdByUserID, discoveryJobID, cleanup)
}

func (r *Repository) createStage(ctx context.Context, createdByUserID, discoveryJobID string, cleanup func(string) error) (Job, bool, error) {
	payload, err := json.Marshal(StageRequest{DiscoveryJobID: discoveryJobID})
	if err != nil {
		return Job{}, false, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, false, err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('routegate-update-stage-admission'))`); err != nil {
		return Job{}, false, err
	}

	existing, err := scanJob(tx.QueryRow(ctx, `
		SELECT
			id::text, operation, status, stage, request_payload, result_payload,
			COALESCE(error_code, ''), COALESCE(created_by_user_id::text, ''),
			created_at, updated_at, started_at, completed_at
		FROM update_jobs
		WHERE operation = $1
		  AND request_payload->>'discoveryJobId' = $2
		  AND (
			status IN ($3, $4)
			OR (
				status = $5
				AND COALESCE(result_payload->>'retention', '') <> $6
			)
		  )
		ORDER BY created_at DESC
		LIMIT 1
	`, OperationStage, discoveryJobID, StatusPending, StatusRunning, StatusSucceeded, stageRetentionEvicted))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Job{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, err
	}

	retained, err := countRetainedStageJobs(ctx, tx)
	if err != nil {
		return Job{}, false, err
	}
	if retained >= maxRetainedStageJobs {
		if cleanup == nil {
			return Job{}, false, ErrStageCapacityExceeded
		}
		victimID, err := oldestRetainedSucceededStage(ctx, tx)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Job{}, false, ErrStageCapacityExceeded
			}
			return Job{}, false, err
		}
		if err := cleanup(victimID); err != nil {
			return Job{}, false, fmt.Errorf("evict retained stage candidate %s: %w", victimID, err)
		}
		commandTag, err := tx.Exec(ctx, `
			UPDATE update_jobs
			SET result_payload = jsonb_set(result_payload, '{retention}', to_jsonb($2::text), true),
			    updated_at = now()
			WHERE id = $1::uuid
			  AND operation = $3
			  AND status = $4
			  AND COALESCE(result_payload->>'retention', '') <> $2
		`, victimID, stageRetentionEvicted, OperationStage, StatusSucceeded)
		if err != nil {
			return Job{}, false, err
		}
		if commandTag.RowsAffected() != 1 {
			return Job{}, false, errors.New("retained stage candidate changed during serialized eviction")
		}
		retained--
	}

	reservedBytes := int64(retained) * stageCandidateReservedBytes()
	nextReservedBytes := reservedBytes + stageCandidateReservedBytes()
	if retained >= maxRetainedStageJobs || nextReservedBytes > maxReservedStageBytes {
		return Job{}, false, ErrStageCapacityExceeded
	}

	job, err := createJobWithQuerier(ctx, tx, OperationStage, StageStage, createdByUserID, payload)
	if err != nil {
		return job, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return job, false, err
	}
	return job, false, nil
}

func stageCandidateReservedBytes() int64 {
	return maxReleaseBundleBytes + maxAttestationBundleBytes + 3*maxSmallReleaseAssetBytes
}

func countRetainedStageJobs(ctx context.Context, tx pgx.Tx) (int, error) {
	var retained int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM update_jobs
		WHERE operation = $1
		  AND (
			status IN ($2, $3)
			OR (
				status = $4
				AND COALESCE(result_payload->>'retention', '') <> $5
			)
		  )
	`, OperationStage, StatusPending, StatusRunning, StatusSucceeded, stageRetentionEvicted).Scan(&retained); err != nil {
		return 0, err
	}
	return retained, nil
}

func oldestRetainedSucceededStage(ctx context.Context, tx pgx.Tx) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM update_jobs
		WHERE operation = $1
		  AND status = $2
		  AND COALESCE(result_payload->>'retention', '') <> $3
		ORDER BY completed_at ASC NULLS FIRST, created_at ASC
		LIMIT 1
		FOR UPDATE
	`, OperationStage, StatusSucceeded, stageRetentionEvicted).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) createJob(ctx context.Context, operation, stage, createdByUserID string, requestPayload json.RawMessage) (Job, error) {
	return createJobWithQuerier(ctx, r.pool, operation, stage, createdByUserID, requestPayload)
}

type jobQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func createJobWithQuerier(ctx context.Context, querier jobQuerier, operation, stage, createdByUserID string, requestPayload json.RawMessage) (Job, error) {
	id, err := newUpdateJobID()
	if err != nil {
		return Job{}, err
	}
	if len(requestPayload) == 0 {
		requestPayload = json.RawMessage(`{}`)
	}
	pending := Job{
		ID:              id,
		Operation:       operation,
		Status:          StatusPending,
		Stage:           stage,
		RequestPayload:  append(json.RawMessage(nil), requestPayload...),
		ResultPayload:   json.RawMessage(`{}`),
		CreatedByUserID: createdByUserID,
	}

	job, err := scanJob(querier.QueryRow(ctx, `
		INSERT INTO update_jobs (
			id,
			operation,
			status,
			stage,
			request_payload,
			created_by_user_id
		)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, NULLIF($6, '')::uuid)
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
	`, id, operation, StatusPending, stage, requestPayload, createdByUserID))
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

func (r *Repository) CompleteStage(ctx context.Context, id string, result StageResult) (Job, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	return r.completeJob(ctx, id, OperationStage, payload)
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
