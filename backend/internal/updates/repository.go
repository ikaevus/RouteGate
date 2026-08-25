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
	maxRetainedStageJobs   = 2
	stageRetentionEvicting = "evicting"
	stageRetentionEvicted  = "evicted"
)

var (
	maxReservedStageBytes   = int64(maxRetainedStageJobs) * stageCandidateReservedBytes()
	ErrStageCapacityExceeded = errors.New("retained stage job capacity exceeded")
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
	return r.createJob(ctx, OperationPreflight, StagePreflight, createdByUserID, json.RawMessage(`{}`))
}

// CreateDiscovery follows the same preallocated-ID durability contract as
// CreatePreflight so an ambiguous INSERT can be reconciled immediately.
func (r *Repository) CreateDiscovery(ctx context.Context, createdByUserID string) (Job, error) {
	return r.createJob(ctx, OperationDiscovery, StageDiscovery, createdByUserID, json.RawMessage(`{}`))
}

func (r *Repository) CreateStage(ctx context.Context, createdByUserID, discoveryJobID string) (Job, bool, error) {
	return r.createStage(ctx, createdByUserID, discoveryJobID, nil)
}

// CreateStageWithCleanup serializes admission across the complete retention
// transition. Eviction is deliberately two-phase: the victim is first durably
// marked "evicting", then its fixed Manager-owned staging directory is removed,
// then the row is marked "evicted" in the same transaction that admits the new
// job. If the process or database fails after cleanup, the durable "evicting"
// marker prevents reuse and makes cleanup safely retryable on the next request.
func (r *Repository) CreateStageWithCleanup(ctx context.Context, createdByUserID, discoveryJobID string, cleanup func(string) error) (Job, bool, error) {
	return r.createStage(ctx, createdByUserID, discoveryJobID, cleanup)
}

func (r *Repository) createStage(ctx context.Context, createdByUserID, discoveryJobID string, cleanup func(string) error) (Job, bool, error) {
	payload, err := json.Marshal(StageRequest{DiscoveryJobID: discoveryJobID})
	if err != nil {
		return Job{}, false, err
	}

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return Job{}, false, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('routegate-update-stage-admission'))`); err != nil {
		return Job{}, false, err
	}
	defer func() {
		if _, unlockErr := conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('routegate-update-stage-admission'))`); unlockErr != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, false, err
	}

	existing, err := reusableStage(ctx, tx, discoveryJobID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Job{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(context.Background())
		return Job{}, false, err
	}

	retained, err := countRetainedStageJobs(ctx, tx)
	if err != nil {
		_ = tx.Rollback(context.Background())
		return Job{}, false, err
	}
	if retained < maxRetainedStageJobs {
		job, err := createStageJobInTx(ctx, tx, createdByUserID, payload, retained)
		if err != nil {
			_ = tx.Rollback(context.Background())
			return job, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return job, false, err
		}
		return job, false, nil
	}
	if cleanup == nil {
		_ = tx.Rollback(context.Background())
		return Job{}, false, ErrStageCapacityExceeded
	}

	victimID, alreadyEvicting, err := evictionCandidate(ctx, tx)
	if err != nil {
		_ = tx.Rollback(context.Background())
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, false, ErrStageCapacityExceeded
		}
		return Job{}, false, err
	}
	if !alreadyEvicting {
		commandTag, err := tx.Exec(ctx, `
			UPDATE update_jobs
			SET result_payload = jsonb_set(result_payload, '{retention}', to_jsonb($2::text), true),
			    updated_at = now()
			WHERE id = $1::uuid
			  AND operation = $3
			  AND status = $4
			  AND COALESCE(result_payload->>'retention', '') NOT IN ($2, $5)
		`, victimID, stageRetentionEvicting, OperationStage, StatusSucceeded, stageRetentionEvicted)
		if err != nil {
			_ = tx.Rollback(context.Background())
			return Job{}, false, err
		}
		if commandTag.RowsAffected() != 1 {
			_ = tx.Rollback(context.Background())
			return Job{}, false, errors.New("retained stage candidate changed during serialized eviction")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, err
	}

	if err := cleanup(victimID); err != nil {
		return Job{}, false, fmt.Errorf("evict retained stage candidate %s: %w", victimID, err)
	}

	finalTx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, false, err
	}
	commandTag, err := finalTx.Exec(ctx, `
		UPDATE update_jobs
		SET result_payload = jsonb_set(result_payload, '{retention}', to_jsonb($2::text), true),
		    updated_at = now()
		WHERE id = $1::uuid
		  AND operation = $3
		  AND status = $4
		  AND result_payload->>'retention' = $5
	`, victimID, stageRetentionEvicted, OperationStage, StatusSucceeded, stageRetentionEvicting)
	if err != nil {
		_ = finalTx.Rollback(context.Background())
		return Job{}, false, err
	}
	if commandTag.RowsAffected() != 1 {
		_ = finalTx.Rollback(context.Background())
		return Job{}, false, errors.New("evicting stage candidate lost its durable retention marker")
	}

	retained, err = countRetainedStageJobs(ctx, finalTx)
	if err != nil {
		_ = finalTx.Rollback(context.Background())
		return Job{}, false, err
	}
	job, err := createStageJobInTx(ctx, finalTx, createdByUserID, payload, retained)
	if err != nil {
		_ = finalTx.Rollback(context.Background())
		return job, false, err
	}
	if err := finalTx.Commit(ctx); err != nil {
		return job, false, err
	}
	return job, false, nil
}

func reusableStage(ctx context.Context, tx pgx.Tx, discoveryJobID string) (Job, error) {
	return scanJob(tx.QueryRow(ctx, `
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
				AND COALESCE(result_payload->>'retention', '') NOT IN ($6, $7)
			)
		  )
		ORDER BY created_at DESC
		LIMIT 1
	`, OperationStage, discoveryJobID, StatusPending, StatusRunning, StatusSucceeded, stageRetentionEvicting, stageRetentionEvicted))
}

func createStageJobInTx(ctx context.Context, tx pgx.Tx, createdByUserID string, payload json.RawMessage, retained int) (Job, error) {
	reservedBytes := int64(retained) * stageCandidateReservedBytes()
	nextReservedBytes := reservedBytes + stageCandidateReservedBytes()
	if retained >= maxRetainedStageJobs || nextReservedBytes > maxReservedStageBytes {
		return Job{}, ErrStageCapacityExceeded
	}
	return createJobWithQuerier(ctx, tx, OperationStage, StageStage, createdByUserID, payload)
}

func stageCandidateReservedBytes() int64 {
	return maxReleaseBundleBytes + 2*maxAttestationBundleBytes + 2*maxSmallReleaseAssetBytes
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

func evictionCandidate(ctx context.Context, tx pgx.Tx) (string, bool, error) {
	var id string
	var retention string
	if err := tx.QueryRow(ctx, `
		SELECT id::text, COALESCE(result_payload->>'retention', '')
		FROM update_jobs
		WHERE operation = $1
		  AND status = $2
		  AND COALESCE(result_payload->>'retention', '') <> $3
		ORDER BY
			CASE WHEN result_payload->>'retention' = $4 THEN 0 ELSE 1 END,
			completed_at ASC NULLS FIRST,
			created_at ASC
		LIMIT 1
		FOR UPDATE
	`, OperationStage, StatusSucceeded, stageRetentionEvicted, stageRetentionEvicting).Scan(&id, &retention); err != nil {
		return "", false, err
	}
	return id, retention == stageRetentionEvicting, nil
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
