package maintenance

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type planRepository interface {
	Create(context.Context, Plan, []byte) (Plan, error)
	Get(context.Context, string) (Plan, error)
	Acquire(context.Context, string, string, []byte, time.Time) (Plan, error)
	Complete(context.Context, string, Report) (Plan, error)
	Fail(context.Context, string, Report, string) (Plan, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, plan Plan, tokenHash []byte) (Plan, error) {
	payload, err := json.Marshal(plan.Payload)
	if err != nil {
		return Plan{}, err
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO maintenance_cleanup_plans (
			mode, selected_categories, plan_payload, confirmation_token_hash,
			created_by_user_id, expires_at
		) VALUES ($1, $2, $3::jsonb, $4, $5::uuid, $6)
		RETURNING id::text, created_at`,
		plan.Mode, plan.SelectedCategories, string(payload), tokenHash, nullableUUID(plan.CreatedByUserID), plan.ExpiresAt,
	).Scan(&plan.ID, &plan.CreatedAt)
	if err != nil {
		return Plan{}, err
	}
	plan.Status = StatusPlanned
	return plan, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Plan, error) {
	row := r.pool.QueryRow(ctx, planSelect+` WHERE id=$1`, id)
	return scanPlan(row)
}

func (r *Repository) Acquire(ctx context.Context, id, userID string, tokenHash []byte, now time.Time) (Plan, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback(ctx)
	plan, err := scanPlan(tx.QueryRow(ctx, planSelect+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return Plan{}, err
	}
	if plan.Status != StatusPlanned || plan.CreatedByUserID != userID {
		return Plan{}, ErrPlanNotExecutable
	}
	if !now.Before(plan.ExpiresAt) {
		if _, err := tx.Exec(ctx, `UPDATE maintenance_cleanup_plans SET status='expired', completed_at=$2 WHERE id=$1`, id, now); err != nil {
			return Plan{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Plan{}, err
		}
		return Plan{}, ErrPlanExpired
	}
	if subtle.ConstantTimeCompare(plan.confirmationHash, tokenHash) != 1 {
		return Plan{}, ErrConfirmationRequired
	}
	if _, err := tx.Exec(ctx, `UPDATE maintenance_cleanup_plans SET status='running', started_at=$2 WHERE id=$1`, id, now); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, err
	}
	plan.Status = StatusRunning
	plan.StartedAt = &now
	return plan, nil
}

func (r *Repository) Complete(ctx context.Context, id string, report Report) (Plan, error) {
	return r.finish(ctx, id, StatusSucceeded, report, "")
}

func (r *Repository) Fail(ctx context.Context, id string, report Report, errorCode string) (Plan, error) {
	return r.finish(ctx, id, StatusFailed, report, errorCode)
}

func (r *Repository) finish(ctx context.Context, id, status string, report Report, errorCode string) (Plan, error) {
	payload, err := json.Marshal(report)
	if err != nil {
		return Plan{}, err
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE maintenance_cleanup_plans
		SET status=$2, report_payload=$3::jsonb, error_code=NULLIF($4, ''), completed_at=$5
		WHERE id=$1 AND status='running'
		RETURNING `+planColumns,
		id, status, string(payload), errorCode, report.CompletedAt,
	)
	return scanPlan(row)
}

const planColumns = `
	id::text, mode, status, selected_categories, plan_payload,
	confirmation_token_hash, COALESCE(created_by_user_id::text, ''), created_at,
	expires_at, started_at, completed_at, report_payload, COALESCE(error_code, '')`

const planSelect = `SELECT ` + planColumns + ` FROM maintenance_cleanup_plans`

type rowScanner interface{ Scan(...any) error }

func scanPlan(row rowScanner) (Plan, error) {
	var plan Plan
	var planPayload []byte
	var reportPayload []byte
	err := row.Scan(
		&plan.ID, &plan.Mode, &plan.Status, &plan.SelectedCategories, &planPayload,
		&plan.confirmationHash, &plan.CreatedByUserID, &plan.CreatedAt, &plan.ExpiresAt,
		&plan.StartedAt, &plan.CompletedAt, &reportPayload, &plan.ErrorCode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, err
	}
	if err := json.Unmarshal(planPayload, &plan.Payload); err != nil {
		return Plan{}, err
	}
	if len(reportPayload) > 0 {
		var report Report
		if err := json.Unmarshal(reportPayload, &report); err != nil {
			return Plan{}, err
		}
		plan.Report = &report
	}
	return plan, nil
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
