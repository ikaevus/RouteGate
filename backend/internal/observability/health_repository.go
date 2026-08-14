package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthRepository struct {
	pool *pgxpool.Pool
}

func NewHealthRepository(pool *pgxpool.Pool) *HealthRepository {
	return &HealthRepository{pool: pool}
}

func (r *HealthRepository) ApplyChecks(ctx context.Context, checks []HealthCheck) error {
	if len(checks) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, check := range checks {
		if err := validateHealthCheck(check); err != nil {
			return err
		}
		var previousState string
		previousExists := true
		err := tx.QueryRow(ctx, `
			SELECT state FROM observability_current_health
			WHERE resource_type=$1 AND resource_id=$2 AND check_key=$3
			FOR UPDATE
		`, check.Resource.Type, check.Resource.ID, check.Key).Scan(&previousState)
		if err == pgx.ErrNoRows {
			previousExists = false
		} else if err != nil {
			return err
		}

		evidence := normalizedHealthEvidence(check.Evidence)
		_, err = tx.Exec(ctx, `
			INSERT INTO observability_current_health (
				resource_type, resource_id, check_key, component, state, required,
				reason_code, summary, recommended_action, evidence, observed_at, expires_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10::jsonb,$11,$12,now())
			ON CONFLICT (resource_type,resource_id,check_key) DO UPDATE SET
				component=EXCLUDED.component,state=EXCLUDED.state,required=EXCLUDED.required,
				reason_code=EXCLUDED.reason_code,summary=EXCLUDED.summary,
				recommended_action=EXCLUDED.recommended_action,evidence=EXCLUDED.evidence,
				observed_at=EXCLUDED.observed_at,expires_at=EXCLUDED.expires_at,updated_at=now()
		`, check.Resource.Type, check.Resource.ID, check.Key, check.Component, string(check.State), check.Required,
			check.ReasonCode, check.Summary, check.RecommendedAction, string(evidence), check.ObservedAt, check.ExpiresAt)
		if err != nil {
			return err
		}

		if !previousExists || previousState != string(check.State) {
			var previous any
			if previousExists {
				previous = previousState
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO observability_health_transitions (
					resource_type,resource_id,check_key,component,previous_state,state,
					reason_code,summary,evidence,observed_at
				) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9::jsonb,$10)
			`, check.Resource.Type, check.Resource.ID, check.Key, check.Component, previous, string(check.State),
				check.ReasonCode, check.Summary, string(evidence), check.ObservedAt)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func validateHealthCheck(check HealthCheck) error {
	if strings.TrimSpace(check.Key)=="" || strings.TrimSpace(check.Resource.Type)=="" || strings.TrimSpace(check.Resource.ID)=="" {
		return fmt.Errorf("health check identity is required")
	}
	if strings.TrimSpace(check.Component)=="" || !check.State.Valid() || check.ObservedAt.IsZero() {
		return fmt.Errorf("health check is invalid")
	}
	if check.ExpiresAt != nil && check.ExpiresAt.Before(check.ObservedAt) {
		return fmt.Errorf("health check expiry precedes observation")
	}
	return nil
}

func normalizedHealthEvidence(evidence json.RawMessage) json.RawMessage {
	if len(evidence)==0 || !json.Valid(evidence) {
		return json.RawMessage(`{}`)
	}
	var object map[string]any
	if err:=json.Unmarshal(evidence,&object); err!=nil || object==nil {
		return json.RawMessage(`{}`)
	}
	return evidence
}
