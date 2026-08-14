package observability

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func (r *AlertRepository) ChangeSeverity(ctx context.Context, alert ActiveAlertRecord, condition AlertCondition, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE observability_alerts
		SET severity=$2, summary=$3, reason_code=NULLIF($4,''), recovery_started_at=NULL,
		    last_evaluated_at=$5, updated_at=now()
		WHERE id=$1::uuid
		  AND condition_state IN ('pending','firing')
		  AND severity <> $2
	`, alert.ID, string(condition.Severity), condition.Summary, condition.ReasonCode, now.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid,$2,$2,$3,$4,NULLIF($5,''),$6)
	`, alert.ID, string(alert.State), string(alert.Severity), string(condition.Severity), condition.ReasonCode, now.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AlertRepository) FireIfPending(ctx context.Context, alert ActiveAlertRecord, condition AlertCondition, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE observability_alerts
		SET condition_state='firing', firing_at=$2, summary=$3, reason_code=NULLIF($4,''),
		    recovery_started_at=NULL, last_evaluated_at=$2, updated_at=now()
		WHERE id=$1::uuid AND condition_state='pending'
	`, alert.ID, now.UTC(), condition.Summary, condition.ReasonCode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid,'pending','firing',$2,$2,NULLIF($3,''),$4)
	`, alert.ID, string(alert.Severity), condition.ReasonCode, now.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AlertRepository) ResolveIfActive(ctx context.Context, alert ActiveAlertRecord, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE observability_alerts
		SET condition_state='resolved', resolved_at=$2, recovery_started_at=NULL,
		    last_evaluated_at=$2, updated_at=now()
		WHERE id=$1::uuid AND condition_state IN ('pending','firing')
	`, alert.ID, now.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid,$2,'resolved',$3,$3,'health_recovered',$4)
	`, alert.ID, string(alert.State), string(alert.Severity), now.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Compile-time assertion keeps this file tied to the concrete PostgreSQL-backed
// repository used by the Manager runtime.
var _ = (*pgxpool.Pool)(nil)
