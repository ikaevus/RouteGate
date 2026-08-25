package updates

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrApplyInProgress = errors.New("an update apply job is already pending or running")

func (r *Repository) CreateApply(ctx context.Context, createdByUserID, stageJobID string) (Job, error) {
	payload, err := json.Marshal(ApplyRequest{StageJobID: stageJobID})
	if err != nil {
		return Job{}, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('routegate-update-apply-admission'))`); err != nil {
		return Job{}, err
	}

	var active int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM update_jobs
		WHERE operation = $1
		  AND status IN ($2, $3)
	`, OperationApply, StatusPending, StatusRunning).Scan(&active); err != nil {
		return Job{}, err
	}
	if active != 0 {
		return Job{}, ErrApplyInProgress
	}

	job, err := createJobWithQuerier(ctx, tx, OperationApply, StageApply, createdByUserID, payload)
	if err != nil {
		return job, err
	}
	if err := tx.Commit(ctx); err != nil {
		return job, err
	}
	return job, nil
}

func (r *Repository) CompleteApply(ctx context.Context, id string, result ApplyResult) (Job, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	return r.completeJob(ctx, id, OperationApply, payload)
}
