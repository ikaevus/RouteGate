package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestMigrationsApplyFromScratchOnPostgreSQL(t *testing.T) {
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
		t.Fatalf("apply migrations from scratch: %v", err)
	}
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("re-run migrations idempotently: %v", err)
	}

	version, err := NewSchemaVersionRepository(pool).AppliedSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read applied schema version: %v", err)
	}
	if version != "000114_agent_runtime_metrics_backfill" {
		t.Fatalf("applied schema version = %q, want 000114_agent_runtime_metrics_backfill", version)
	}

	rows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'deliveries'
	`)
	if err != nil {
		t.Fatalf("list deliveries columns: %v", err)
	}
	defer rows.Close()

	forbidden := map[string]struct{}{
		"message_body":          {},
		"html_body":             {},
		"vless_uri":             {},
		"vless_link":            {},
		"connect_url":           {},
		"qr_payload":            {},
		"qr_image":              {},
		"credentials":           {},
		"provider_raw_response": {},
		"provider_raw_error":    {},
	}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan deliveries column: %v", err)
		}
		if _, disallowed := forbidden[column]; disallowed {
			t.Fatalf("deliveries contains forbidden sensitive payload column %q", column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate deliveries columns: %v", err)
	}
}

func TestRuntimeMetricsBackfillMigrationUpgradesLegacyAgentRows(t *testing.T) {
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
	preRuntimeDir := copyMigrationsBefore(t, "../../migrations", "000112_agent_runtime_metrics.up.sql")
	if err := Migrate(ctx, pool, preRuntimeDir, logger); err != nil {
		t.Fatalf("apply pre-runtime migrations: %v", err)
	}

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status)
		VALUES ('Legacy runtime fixture', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create legacy server: %v", err)
	}

	protocolVersion := 1
	now := time.Now().UTC()
	collectedAt := "2026-08-11T20:00:00Z"
	_, err = agents.NewRepository(pool).CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "legacy-runtime-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "legacy-test",
		ProtocolVersion: &protocolVersion,
		TokenHash:       "legacy-runtime-token-hash",
		Capabilities: agents.Capabilities{
			"vpnCore": true,
			"runtimeMetrics": map[string]any{
				"load1":       1.25,
				"load5":       0.75,
				"load15":      0.5,
				"logicalCpus": 4,
				"collectedAt": collectedAt,
			},
		},
		Status:       agents.StatusOnline,
		RegisteredAt: &now,
		LastSeenAt:   &now,
	})
	if err != nil {
		t.Fatalf("create legacy agent: %v", err)
	}

	var legacyRuntimeBlock bool
	if err := pool.QueryRow(ctx, `
		SELECT capabilities ? 'runtimeMetrics'
		FROM agents
		WHERE server_id = $1::uuid
	`, serverID).Scan(&legacyRuntimeBlock); err != nil {
		t.Fatalf("read legacy capabilities: %v", err)
	}
	if !legacyRuntimeBlock {
		t.Fatal("fixture must contain legacy runtimeMetrics before migration 112")
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply runtime/delivery/backfill migrations: %v", err)
	}

	var (
		hasRuntimeBlock bool
		staticVPNCore   bool
		load1           float64
		load5           float64
		load15          float64
		logicalCPUs     int
		runtimeTime     time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT
			capabilities ? 'runtimeMetrics',
			COALESCE((capabilities ->> 'vpnCore')::boolean, false),
			runtime_load_1,
			runtime_load_5,
			runtime_load_15,
			runtime_logical_cpus,
			runtime_collected_at
		FROM agents
		WHERE server_id = $1::uuid
	`, serverID).Scan(
		&hasRuntimeBlock,
		&staticVPNCore,
		&load1,
		&load5,
		&load15,
		&logicalCPUs,
		&runtimeTime,
	); err != nil {
		t.Fatalf("read backfilled runtime metrics: %v", err)
	}

	if hasRuntimeBlock {
		t.Fatal("runtimeMetrics must be removed from durable capabilities")
	}
	if !staticVPNCore {
		t.Fatal("static capabilities must survive runtime backfill")
	}
	if load1 != 1.25 || load5 != 0.75 || load15 != 0.5 || logicalCPUs != 4 {
		t.Fatalf("unexpected backfilled runtime values: load1=%v load5=%v load15=%v cpus=%d", load1, load5, load15, logicalCPUs)
	}
	if runtimeTime.UTC().Format(time.RFC3339) != collectedAt {
		t.Fatalf("runtime collectedAt = %s, want %s", runtimeTime.UTC().Format(time.RFC3339), collectedAt)
	}

	version, err := NewSchemaVersionRepository(pool).AppliedSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read applied schema version: %v", err)
	}
	if version != "000114_agent_runtime_metrics_backfill" {
		t.Fatalf("applied schema version = %q, want 000114_agent_runtime_metrics_backfill", version)
	}
}

func resetPublicSchema(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (interface{ RowsAffected() int64 }, error)
}) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}
}

func copyMigrationsBefore(t *testing.T, sourceDir, stopBefore string) string {
	t.Helper()
	destination := t.TempDir()
	files, err := filepath.Glob(filepath.Join(sourceDir, "*.up.sql"))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	for _, file := range files {
		base := filepath.Base(file)
		if base >= stopBefore {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", base, err)
		}
		if err := os.WriteFile(filepath.Join(destination, base), content, 0o600); err != nil {
			t.Fatalf("copy migration %s: %v", base, err)
		}
	}
	return destination
}
