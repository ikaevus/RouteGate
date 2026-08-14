package observability

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
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

	var transitionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid,$2,$2,$3,$4,NULLIF($5,''),$6)
		RETURNING id::text
	`, alert.ID, string(alert.State), string(alert.Severity), string(condition.Severity), condition.ReasonCode, now.UTC()).Scan(&transitionID); err != nil {
		return err
	}

	// Only a severity increase on an already-firing incident is externally
	// actionable. Pending severity changes are still durable transitions, but
	// they have not become an incident notification yet.
	if alert.State == AlertFiring && severityRank(condition.Severity) > severityRank(alert.Severity) {
		if err := insertNotificationIntent(ctx, tx, notificationIntentInput{
			AlertID:          alert.ID,
			AlertTransitionID: transitionID,
			Kind:             "escalated",
			Severity:         condition.Severity,
			RuleKey:          condition.RuleKey,
			Resource:         condition.Resource,
			ReasonCode:       condition.ReasonCode,
			Summary:          condition.Summary,
		}); err != nil {
			return err
		}
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

	var transitionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid,'pending','firing',$2,$2,NULLIF($3,''),$4)
		RETURNING id::text
	`, alert.ID, string(alert.Severity), condition.ReasonCode, now.UTC()).Scan(&transitionID); err != nil {
		return err
	}
	if err := insertNotificationIntent(ctx, tx, notificationIntentInput{
		AlertID:          alert.ID,
		AlertTransitionID: transitionID,
		Kind:             "firing",
		Severity:         alert.Severity,
		RuleKey:          condition.RuleKey,
		Resource:         condition.Resource,
		ReasonCode:       condition.ReasonCode,
		Summary:          condition.Summary,
	}); err != nil {
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

	var transitionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO observability_alert_transitions (
			alert_id, from_state, to_state, from_severity, to_severity, reason_code, occurred_at
		) VALUES ($1::uuid,$2,'resolved',$3,$3,'health_recovered',$4)
		RETURNING id::text
	`, alert.ID, string(alert.State), string(alert.Severity), now.UTC()).Scan(&transitionID); err != nil {
		return err
	}

	// Pending episodes that clear before firing are intentionally silent. Only a
	// real incident that had reached firing produces a resolved notification.
	if alert.State == AlertFiring {
		if err := insertNotificationIntent(ctx, tx, notificationIntentInput{
			AlertID:          alert.ID,
			AlertTransitionID: transitionID,
			Kind:             "resolved",
			Severity:         alert.Severity,
			RuleKey:          alert.RuleKey,
			Resource:         alert.Resource,
			ReasonCode:       alert.ReasonCode,
			Summary:          alert.Summary,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type notificationIntentInput struct {
	AlertID           string
	AlertTransitionID string
	Kind              string
	Severity          Severity
	RuleKey           string
	Resource          ResourceRef
	ReasonCode        string
	Summary           string
}

func insertNotificationIntent(ctx context.Context, tx pgx.Tx, input notificationIntentInput) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO observability_notification_intents (
			alert_id, alert_transition_id, kind, severity, rule_key,
			resource_type, resource_id, reason_code, summary
		) VALUES (
			$1::uuid, $2::uuid, $3, $4, $5,
			$6, $7, NULLIF($8,''), $9
		)
		ON CONFLICT (alert_transition_id) DO NOTHING
	`, input.AlertID, input.AlertTransitionID, input.Kind, string(input.Severity), input.RuleKey,
		input.Resource.Type, input.Resource.ID, input.ReasonCode, input.Summary)
	return err
}
