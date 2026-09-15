package maintenance

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

// RecoverInterruptedPlans releases the single-run interlock after a Manager
// restart. It never retries cleanup: the previous outcome may be partial, so an
// administrator must analyze current storage and create a fresh immutable plan.
func RecoverInterruptedPlans(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool) error {
	now := time.Now().UTC()
	report, err := json.Marshal(Report{
		SchemaVersion: 1,
		Status:        StatusFailed,
		StartedAt:     now,
		CompletedAt:   now,
		Providers:     []ProviderReport{},
	})
	if err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `
		UPDATE maintenance_cleanup_plans
		SET status='failed', completed_at=$1, report_payload=$2::jsonb,
			error_code='interrupted_cleanup_outcome_unknown'
		WHERE status='running'
		RETURNING id::text`, now, string(report))
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	recorder := audit.NewRecorder(logger, pool)
	for _, id := range ids {
		recorder.RecordSafe(ctx, audit.EventInput{
			ActorType: audit.ActorTypeSystem, Action: "maintenance.cleanup.interrupted",
			ResourceType: "maintenance_cleanup_plan", ResourceID: id, Result: audit.ResultFailure,
			Metadata: map[string]any{"outcome": "unknown", "automatic_retry": false},
		})
	}
	return nil
}
