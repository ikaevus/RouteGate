package observability

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActiveAlertRecord struct {
	ID                string
	Fingerprint       string
	RuleKey           string
	Resource          ResourceRef
	Severity          Severity
	State             AlertState
	Summary           string
	ReasonCode        string
	StartedAt         time.Time
	FiringAt          *time.Time
	LastEvaluatedAt   time.Time
	RecoveryStartedAt *time.Time
}

type AlertRepository struct {
	pool *pgxpool.Pool
}

func NewAlertRepository(pool *pgxpool.Pool) *AlertRepository {
	return &AlertRepository{pool: pool}
}

func (r *AlertRepository) ListCurrentHealth(ctx context.Context) ([]HealthCheck, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT resource_type, resource_id, check_key, component, state, required,
		       COALESCE(reason_code, ''), COALESCE(summary, ''), COALESCE(recommended_action, ''),
		       evidence, observed_at, expires_at
		FROM observability_current_health
		ORDER BY resource_type, resource_id, check_key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	checks := make([]HealthCheck, 0)
	for rows.Next() {
		var check HealthCheck
		var state string
		var evidence []byte
		if err := rows.Scan(
			&check.Resource.Type,
			&check.Resource.ID,
			&check.Key,
			&check.Component,
			&state,
			&check.Required,
			&check.ReasonCode,
			&check.Summary,
			&check.RecommendedAction,
			&evidence,
			&check.ObservedAt,
			&check.ExpiresAt,
		); err != nil {
			return nil, err
		}
		check.State = HealthState(state)
		check.Evidence = json.RawMessage(evidence)
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func (r *AlertRepository) ActiveByFingerprint(ctx context.Context, fingerprint string) (*ActiveAlertRecord, error) {
	return scanActiveAlert(r.pool.QueryRow(ctx, `
		SELECT id::text, fingerprint, rule_key, resource_type, resource_id,
		       severity, condition_state, summary, COALESCE(reason_code, ''),
		       started_at, firing_at, last_evaluated_at, recovery_started_at
		FROM observability_alerts
		WHERE fingerprint=$1 AND condition_state IN ('pending','firing')
	`, fingerprint))
}

func (r *AlertRepository) LastResolvedAt(ctx context.Context, fingerprint string) (*time.Time, error) {
	var resolvedAt *time.Time
	if err := r.pool.QueryRow(ctx, `
		SELECT MAX(resolved_at)
		FROM observability_alerts
		WHERE fingerprint=$1 AND condition_state='resolved'
	`, fingerprint).Scan(&resolvedAt); err != nil {
		return nil, err
	}
	return resolvedAt, nil
}

func (r *AlertRepository) CreatePending(ctx context.Context, condition AlertCondition, now time.Time) (*ActiveAlertRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO observability_alerts (
			fingerprint, rule_key, resource_type, resource_id, severity,
			condition_state, summary, reason_code, started_at, last_evaluated_at
		) VALUES ($1,$2,$3,$4,$5,'pending',$6,NULLIF($7,''),$8,$8)
		ON CONFLICT (fingerprint) WHERE condition_state IN ('pending','firing') DO NOTHING
		RETURNING id::text, fingerprint, rule_key, resource_type, resource_id,
		          severity, condition_state, summary, COALESCE(reason_code, ''),
		          started_at, firing_at, last_evaluated_at, recovery_started_at
	`, condition.Fingerprint, condition.RuleKey, condition.Resource.Type, condition.Resource.ID,
		string(condition.Severity), condition.Summary, condition.ReasonCode, now.UTC())
	alert, err := scanActiveAlert(row)
	if err == pgx.ErrNoRows {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return r.ActiveByFingerprint(ctx, condition.Fingerprint)
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid, NULL, 'pending', NULL, $2, NULLIF($3,''), $4)
	`, alert.ID, string(alert.Severity), alert.ReasonCode, now.UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return alert, nil
}

func (r *AlertRepository) Touch(ctx context.Context, alertID string, condition AlertCondition, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE observability_alerts
		SET summary=$2, reason_code=NULLIF($3,''), last_evaluated_at=$4, updated_at=now()
		WHERE id=$1::uuid AND condition_state IN ('pending','firing')
	`, alertID, condition.Summary, condition.ReasonCode, now.UTC())
	return err
}

func (r *AlertRepository) StartRecovery(ctx context.Context, alertID string, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE observability_alerts
		SET recovery_started_at=COALESCE(recovery_started_at,$2), last_evaluated_at=$2, updated_at=now()
		WHERE id=$1::uuid AND condition_state IN ('pending','firing')
	`, alertID, now.UTC())
	return err
}

func (r *AlertRepository) ClearRecovery(ctx context.Context, alertID string, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE observability_alerts
		SET recovery_started_at=NULL, last_evaluated_at=$2, updated_at=now()
		WHERE id=$1::uuid AND condition_state IN ('pending','firing')
	`, alertID, now.UTC())
	return err
}

func (r *AlertRepository) Escalate(ctx context.Context, alert ActiveAlertRecord, condition AlertCondition, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE observability_alerts
		SET severity=$2, summary=$3, reason_code=NULLIF($4,''), recovery_started_at=NULL,
		    last_evaluated_at=$5, updated_at=now()
		WHERE id=$1::uuid AND condition_state IN ('pending','firing')
	`, alert.ID, string(condition.Severity), condition.Summary, condition.ReasonCode, now.UTC()); err != nil {
		return err
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

func (r *AlertRepository) Fire(ctx context.Context, alert ActiveAlertRecord, condition AlertCondition, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE observability_alerts
		SET condition_state='firing', firing_at=$2, summary=$3, reason_code=NULLIF($4,''),
		    recovery_started_at=NULL, last_evaluated_at=$2, updated_at=now()
		WHERE id=$1::uuid AND condition_state='pending'
	`, alert.ID, now.UTC(), condition.Summary, condition.ReasonCode); err != nil {
		return err
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

func (r *AlertRepository) Resolve(ctx context.Context, alert ActiveAlertRecord, now time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE observability_alerts
		SET condition_state='resolved', resolved_at=$2, recovery_started_at=NULL,
		    last_evaluated_at=$2, updated_at=now()
		WHERE id=$1::uuid AND condition_state IN ('pending','firing')
	`, alert.ID, now.UTC()); err != nil {
		return err
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

func scanActiveAlert(row pgx.Row) (*ActiveAlertRecord, error) {
	var alert ActiveAlertRecord
	var severity string
	var state string
	if err := row.Scan(
		&alert.ID,
		&alert.Fingerprint,
		&alert.RuleKey,
		&alert.Resource.Type,
		&alert.Resource.ID,
		&severity,
		&state,
		&alert.Summary,
		&alert.ReasonCode,
		&alert.StartedAt,
		&alert.FiringAt,
		&alert.LastEvaluatedAt,
		&alert.RecoveryStartedAt,
	); err != nil {
		return nil, err
	}
	alert.Severity = Severity(severity)
	alert.State = AlertState(state)
	return &alert, nil
}
