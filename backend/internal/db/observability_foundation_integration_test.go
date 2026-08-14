package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"
)

func TestObservabilityFoundationPersistenceBoundaries(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE 'observability_%' ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list observability tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan observability table: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate observability tables: %v", err)
	}
	wantTables := []string{
		"observability_agent_telemetry",
		"observability_alert_acknowledgements",
		"observability_alert_transitions",
		"observability_alerts",
		"observability_current_health",
		"observability_diagnostic_runs",
		"observability_events",
		"observability_health_transitions",
		"observability_notification_deliveries",
		"observability_notification_intents",
	}
	if !slices.Equal(tables, wantTables) {
		t.Fatalf("observability tables = %v, want %v; raw metrics samples must not become PostgreSQL product state", tables, wantTables)
	}

	var alertID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO observability_alerts (fingerprint,rule_key,resource_type,resource_id,severity,condition_state,summary,started_at,firing_at,last_evaluated_at)
		VALUES ('host.disk.capacity:server-1','host.disk.capacity.critical','server','server-1','critical','firing','Disk usage critically high',now(),now(),now())
		RETURNING id::text
	`).Scan(&alertID); err != nil {
		t.Fatalf("create firing alert episode: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observability_alert_acknowledgements (alert_id,note) VALUES ($1::uuid,'Administrator is investigating')`, alertID); err != nil {
		t.Fatalf("acknowledge firing alert: %v", err)
	}
	var conditionState string
	if err := pool.QueryRow(ctx, `SELECT condition_state FROM observability_alerts WHERE id=$1::uuid`, alertID).Scan(&conditionState); err != nil {
		t.Fatalf("read acknowledged alert: %v", err)
	}
	if conditionState != "firing" {
		t.Fatalf("acknowledged alert condition_state = %q, want firing", conditionState)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO observability_alerts (fingerprint,rule_key,resource_type,resource_id,severity,condition_state,summary,started_at,firing_at,last_evaluated_at)
		VALUES ('host.disk.capacity:server-1','host.disk.capacity.critical','server','server-1','critical','firing','Duplicate active episode',now(),now(),now())
	`); err == nil {
		t.Fatal("duplicate active alert fingerprint must be rejected")
	}
	if _, err := pool.Exec(ctx, `UPDATE observability_alerts SET condition_state='resolved',resolved_at=now(),last_evaluated_at=now(),updated_at=now() WHERE id=$1::uuid`, alertID); err != nil {
		t.Fatalf("resolve first alert episode: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO observability_alerts (fingerprint,rule_key,resource_type,resource_id,severity,condition_state,summary,started_at,firing_at,last_evaluated_at)
		VALUES ('host.disk.capacity:server-1','host.disk.capacity.critical','server','server-1','critical','firing','Disk usage critically high again',now(),now(),now())
	`); err != nil {
		t.Fatalf("create recurrence after resolved episode: %v", err)
	}
}
