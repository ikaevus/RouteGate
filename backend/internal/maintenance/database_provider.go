package maintenance

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type databaseProvider struct {
	pool        *pgxpool.Pool
	category    Category
	countQuery  string
	deleteQuery string
}

func (p databaseProvider) Category() Category { return p.category }

func (p databaseProvider) Analyze(ctx context.Context, cutoff time.Time) (PlanItem, error) {
	count, err := p.count(ctx, cutoff)
	if err != nil {
		return PlanItem{}, fmt.Errorf("analyze %s: %w", p.category.ID, err)
	}
	return PlanItem{
		CategoryID: p.category.ID, Scope: p.category.Scope,
		RetentionDays: p.category.RetentionDays, Cutoff: cutoff, CandidateCount: count,
	}, nil
}

func (p databaseProvider) Cleanup(ctx context.Context, item PlanItem) (CleanupResult, error) {
	var deleted int64
	if err := p.pool.QueryRow(ctx, p.deleteQuery, item.Cutoff).Scan(&deleted); err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup %s: %w", p.category.ID, err)
	}
	return CleanupResult{DeletedCount: deleted}, nil
}

func (p databaseProvider) Verify(ctx context.Context, item PlanItem) (int64, error) {
	return p.count(ctx, item.Cutoff)
}

func (p databaseProvider) count(ctx context.Context, cutoff time.Time) (int64, error) {
	var count int64
	err := p.pool.QueryRow(ctx, p.countQuery, cutoff).Scan(&count)
	return count, err
}

func databaseProviders(pool *pgxpool.Pool) []Provider {
	return []Provider{
		databaseProvider{
			pool:     pool,
			category: Category{ID: "expired_ephemeral_data", Scope: "postgresql", Recommended: true, Selectable: true, RetentionDays: 7},
			countQuery: `
				SELECT
					(SELECT count(*) FROM auth_sessions WHERE expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $1)) +
					(SELECT count(*) FROM initial_setup_tokens WHERE expires_at < $1 OR (used_at IS NOT NULL AND used_at < $1)) +
					(SELECT count(*) FROM server_registration_tokens WHERE (expires_at IS NOT NULL AND expires_at < $1) OR (used_at IS NOT NULL AND used_at < $1)) +
					(SELECT count(*) FROM telegram_pairing_sessions WHERE expires_at < $1 OR (consumed_at IS NOT NULL AND consumed_at < $1))`,
			deleteQuery: `
				WITH
				a AS (DELETE FROM auth_sessions WHERE expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $1) RETURNING 1),
				b AS (DELETE FROM initial_setup_tokens WHERE expires_at < $1 OR (used_at IS NOT NULL AND used_at < $1) RETURNING 1),
				c AS (DELETE FROM server_registration_tokens WHERE (expires_at IS NOT NULL AND expires_at < $1) OR (used_at IS NOT NULL AND used_at < $1) RETURNING 1),
				d AS (DELETE FROM telegram_pairing_sessions WHERE expires_at < $1 OR (consumed_at IS NOT NULL AND consumed_at < $1) RETURNING 1)
				SELECT (SELECT count(*) FROM a) + (SELECT count(*) FROM b) + (SELECT count(*) FROM c) + (SELECT count(*) FROM d)`,
		},
		databaseProvider{
			pool:        pool,
			category:    Category{ID: "agent_heartbeat_history", Scope: "postgresql", Recommended: true, Selectable: true, RetentionDays: 30},
			countQuery:  `SELECT count(*) FROM agent_heartbeats WHERE created_at < $1`,
			deleteQuery: `WITH deleted AS (DELETE FROM agent_heartbeats WHERE created_at < $1 RETURNING 1) SELECT count(*) FROM deleted`,
		},
		databaseProvider{
			pool:        pool,
			category:    Category{ID: "diagnostic_history", Scope: "postgresql", Recommended: true, Selectable: true, RetentionDays: 30},
			countQuery:  `SELECT count(*) FROM observability_diagnostic_runs WHERE status IN ('succeeded', 'failed') AND completed_at < $1`,
			deleteQuery: `WITH deleted AS (DELETE FROM observability_diagnostic_runs WHERE status IN ('succeeded', 'failed') AND completed_at < $1 RETURNING 1) SELECT count(*) FROM deleted`,
		},
		databaseProvider{
			pool:     pool,
			category: Category{ID: "observability_history", Scope: "postgresql", Selectable: true, RetentionDays: 90},
			countQuery: `SELECT
				(SELECT count(*) FROM observability_health_transitions WHERE observed_at < $1) +
				(SELECT count(*) FROM observability_events WHERE occurred_at < $1)`,
			deleteQuery: `WITH
				a AS (DELETE FROM observability_health_transitions WHERE observed_at < $1 RETURNING 1),
				b AS (DELETE FROM observability_events WHERE occurred_at < $1 RETURNING 1)
				SELECT (SELECT count(*) FROM a) + (SELECT count(*) FROM b)`,
		},
		databaseProvider{
			pool:        pool,
			category:    Category{ID: "audit_history", Scope: "postgresql", Selectable: true, RetentionDays: 365},
			countQuery:  `SELECT count(*) FROM audit_events WHERE created_at < $1`,
			deleteQuery: `WITH deleted AS (DELETE FROM audit_events WHERE created_at < $1 RETURNING 1) SELECT count(*) FROM deleted`,
		},
	}
}
