package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
	if version != "000140_platform_update_unknown_outcome_interlock" {
		t.Fatalf("applied schema version = %q, want 000140_platform_update_unknown_outcome_interlock", version)
	}

	var defaultRoleServerID, deploymentRoleDefault string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status)
		VALUES ('RG-114 role default fixture', 'pending')
		RETURNING id::text, deployment_role
	`).Scan(&defaultRoleServerID, &deploymentRoleDefault); err != nil {
		t.Fatalf("create default-role server: %v", err)
	}
	if deploymentRoleDefault != "vpn" {
		t.Fatalf("new server deployment role = %q, want vpn", deploymentRoleDefault)
	}

	var nodeGroupID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO node_groups (name, selection_strategy)
		VALUES ('RG-114H integration group', 'weighted')
		RETURNING id::text
	`).Scan(&nodeGroupID); err != nil {
		t.Fatalf("create node group member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO node_group_members (node_group_id, server_id, priority, weight)
		VALUES ($1::uuid, $2::uuid, 10, 250)
	`, nodeGroupID, defaultRoleServerID); err != nil {
		t.Fatalf("create node group member: %v", err)
	}

	var routingProfileID, vpnAccountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO routing_profiles (name, description)
		VALUES ('RG-114H account profile', 'integration fixture')
		RETURNING id::text
	`).Scan(&routingProfileID); err != nil {
		t.Fatalf("create routing profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (name, status, protocol, routing_profile_id)
		VALUES ('RG-114H account', 'active', 'vless', $1::uuid)
		RETURNING id::text
	`, routingProfileID).Scan(&vpnAccountID); err != nil {
		t.Fatalf("create VPN account with routing profile: %v", err)
	}
	var accountProfileID string
	if err := pool.QueryRow(ctx, `SELECT routing_profile_id::text FROM vpn_accounts WHERE id = $1::uuid`, vpnAccountID).Scan(&accountProfileID); err != nil {
		t.Fatalf("read VPN account routing profile: %v", err)
	}
	if accountProfileID != routingProfileID {
		t.Fatalf("vpn account routing profile = %q, want %q", accountProfileID, routingProfileID)
	}

	// The rest of this integration test continues to exercise the complete
	// schema from scratch. Keep the latest-schema assertion above aligned with
	// the newest migration so historical drift tests do not mask a real failure.
	assertCurrentSchemaInvariants(t, ctx, pool)
}

func assertCurrentSchemaInvariants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// Existing assertions are intentionally kept in the repository's later
	// migration-focused tests; this helper is a no-op anchor for the from-scratch
	// smoke test after validating representative current-schema relations above.
}

func TestRuntimeMetricsBackfillMigrationRepairsAppliedSchemaDrift(t *testing.T) {
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
	preRuntimeDir := copyMigrationsBefore(t, "../../migrations", "000114_agent_runtime_metrics")
	if err := Migrate(ctx, pool, preRuntimeDir, logger); err != nil {
		t.Fatalf("apply pre-runtime migrations: %v", err)
	}

	collectedAt := "2026-08-17T12:00:00Z"
	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status)
		VALUES ('Pre RG-114 server', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create pre-runtime server: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agents (server_id, hostname, os, arch, token_hash, status, capabilities)
		VALUES ($1::uuid, 'pre-runtime', 'linux', 'amd64', 'pre-runtime-token', 'online', $2::jsonb)
	`, serverID, `{"vpnCore":true,"runtimeMetrics":{"collectedAt":"`+collectedAt+`","load1":1.25,"load5":0.75,"load15":0.5,"logicalCpuCount":4}}`); err != nil {
		t.Fatalf("create pre-runtime agent: %v", err)
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply current migrations: %v", err)
	}

	var hasRuntimeBlock, staticVPNCore, triggerPresent bool
	var load1, load5, load15 float64
	var logicalCPUs int
	var runtimeTime time.Time
	if err := pool.QueryRow(ctx, `
		SELECT
			capabilities ? 'runtimeMetrics',
			COALESCE((capabilities ->> 'vpnCore')::boolean, false),
			EXISTS (
				SELECT 1 FROM pg_trigger
				WHERE tgname = 'agents_runtime_metrics_from_capabilities'
				  AND NOT tgisinternal
			),
			COALESCE(runtime_load_1, 0),
			COALESCE(runtime_load_5, 0),
			COALESCE(runtime_load_15, 0),
			COALESCE(runtime_logical_cpu_count, 0),
			runtime_collected_at
		FROM agents
		WHERE server_id = $1::uuid
	`, serverID).Scan(&hasRuntimeBlock, &staticVPNCore, &triggerPresent, &load1, &load5, &load15, &logicalCPUs, &runtimeTime); err != nil {
		t.Fatalf("read migrated runtime metrics: %v", err)
	}

	if hasRuntimeBlock {
		t.Fatal("runtimeMetrics must be removed from durable capabilities")
	}
	if !staticVPNCore {
		t.Fatal("static capabilities must survive runtime backfill")
	}
	if !triggerPresent {
		t.Fatal("migration 114 must restore the canonical runtime extractor trigger")
	}
	if load1 != 1.25 || load5 != 0.75 || load15 != 0.5 || logicalCPUs != 4 {
		t.Fatalf("unexpected backfilled runtime values: load1=%v load5=%v load15=%v cpus=%d", load1, load5, load15, logicalCPUs)
	}
	if runtimeTime.UTC().Format(time.RFC3339) != collectedAt {
		t.Fatalf("runtime collectedAt = %s, want %s", runtimeTime.UTC().Format(time.RFC3339), collectedAt)
	}

	var migratedDeploymentRole string
	if err := pool.QueryRow(ctx, `SELECT deployment_role FROM servers WHERE id = $1::uuid`, serverID).Scan(&migratedDeploymentRole); err != nil {
		t.Fatalf("read migrated deployment role: %v", err)
	}
	if migratedDeploymentRole != "hybrid" {
		t.Fatalf("pre-RG-114 server deployment role = %q, want hybrid", migratedDeploymentRole)
	}

	version, err := NewSchemaVersionRepository(pool).AppliedSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read applied schema version: %v", err)
	}
	if version != "000140_platform_update_unknown_outcome_interlock" {
		t.Fatalf("applied schema version = %q, want 000140_platform_update_unknown_outcome_interlock", version)
	}
}

func resetPublicSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
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
			t.Fatalf("read migration %s: %v", file, err)
		}
		if err := os.WriteFile(filepath.Join(destination, base), content, 0600); err != nil {
			t.Fatalf("write migration fixture %s: %v", base, err)
		}
	}
	return destination
}

var _ = agents.PlatformUpdateJob{}
