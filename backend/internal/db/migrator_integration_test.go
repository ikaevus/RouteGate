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
	if version != "000130_multi_protocol_account_profiles" {
		t.Fatalf("applied schema version = %q, want 000130_multi_protocol_account_profiles", version)
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
		t.Fatalf("create node group: %v", err)
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
		t.Fatalf("create account routing profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('rg114h-fixture', 'sing-box', 'RG-114H fixture', 'active', $1::uuid)
		RETURNING id::text
	`, defaultRoleServerID).Scan(&vpnAccountID); err != nil {
		t.Fatalf("create VPN account routing fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_routing_profiles (vpn_account_id, routing_profile_id)
		VALUES ($1::uuid, $2::uuid)
	`, vpnAccountID, routingProfileID); err != nil {
		t.Fatalf("create VPN account routing profile assignment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_node_groups (vpn_account_id, node_group_id)
		VALUES ($1::uuid, $2::uuid)
	`, vpnAccountID, nodeGroupID); err != nil {
		t.Fatalf("create VPN account routing policy: %v", err)
	}
	var automaticSelectionEnabled, allowDegraded bool
	var cooldownSeconds int
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_account_automatic_selection_policies (vpn_account_id, enabled)
		VALUES ($1::uuid, TRUE)
		RETURNING enabled, allow_degraded, cooldown_seconds
	`, vpnAccountID).Scan(&automaticSelectionEnabled, &allowDegraded, &cooldownSeconds); err != nil {
		t.Fatalf("create automatic selection policy: %v", err)
	}
	if !automaticSelectionEnabled || allowDegraded || cooldownSeconds != 300 {
		t.Fatalf("unexpected automatic selection defaults: enabled=%v allow_degraded=%v cooldown=%d", automaticSelectionEnabled, allowDegraded, cooldownSeconds)
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

	providerRows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'delivery_provider_settings'
	`)
	if err != nil {
		t.Fatalf("list delivery_provider_settings columns: %v", err)
	}
	defer providerRows.Close()

	allowedSecretStorage := map[string]struct{}{
		"secret_ciphertext":  {},
		"secret_nonce":       {},
		"secret_key_version": {},
	}
	for providerRows.Next() {
		var column string
		if err := providerRows.Scan(&column); err != nil {
			t.Fatalf("scan delivery_provider_settings column: %v", err)
		}
		if column == "password" || column == "token" || column == "access_token" || column == "credentials" || column == "secret" {
			t.Fatalf("delivery_provider_settings contains plaintext secret column %q", column)
		}
		if len(column) >= 7 && column[:7] == "secret_" {
			if _, allowed := allowedSecretStorage[column]; !allowed {
				t.Fatalf("delivery_provider_settings contains unexpected secret column %q", column)
			}
		}
	}
	if err := providerRows.Err(); err != nil {
		t.Fatalf("iterate delivery_provider_settings columns: %v", err)
	}

	pairingRows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema='public'
		  AND table_name='telegram_pairing_sessions'
	`)
	if err != nil {
		t.Fatalf("list telegram_pairing_sessions columns: %v", err)
	}
	defer pairingRows.Close()
	pairingColumns := map[string]bool{}
	for pairingRows.Next() {
		var column string
		if err := pairingRows.Scan(&column); err != nil {
			t.Fatalf("scan telegram_pairing_sessions column: %v", err)
		}
		pairingColumns[column] = true
		if column == "start_parameter" || column == "token" || column == "pairing_token" || column == "secret" {
			t.Fatalf("telegram_pairing_sessions contains plaintext pairing secret column %q", column)
		}
	}
	if err := pairingRows.Err(); err != nil {
		t.Fatalf("iterate telegram_pairing_sessions columns: %v", err)
	}
	if !pairingColumns["start_parameter_hash"] {
		t.Fatal("telegram_pairing_sessions must store only the start parameter hash")
	}
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

	preBackfillDir := copyMigrationsBefore(t, "../../migrations", "000114_agent_runtime_metrics_backfill.up.sql")
	if err := Migrate(ctx, pool, preBackfillDir, logger); err != nil {
		t.Fatalf("apply migrations through 113: %v", err)
	}

	var versionBeforeBackfill string
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1`).Scan(&versionBeforeBackfill); err != nil {
		t.Fatalf("read schema version before backfill: %v", err)
	}
	if versionBeforeBackfill != "000113_delivery_foundation" {
		t.Fatalf("schema version before backfill = %q, want 000113_delivery_foundation", versionBeforeBackfill)
	}
	if err := pool.QueryRow(ctx, `
		SELECT capabilities ? 'runtimeMetrics'
		FROM agents
		WHERE server_id = $1::uuid
	`, serverID).Scan(&legacyRuntimeBlock); err != nil {
		t.Fatalf("read capabilities after migration 113: %v", err)
	}
	if !legacyRuntimeBlock {
		t.Fatal("legacy row must remain untouched before migration 114")
	}

	if _, err := pool.Exec(ctx, `
		DROP TRIGGER IF EXISTS agents_extract_runtime_metrics ON agents;
		DROP FUNCTION IF EXISTS routegate_extract_agent_runtime_metrics();
	`); err != nil {
		t.Fatalf("simulate applied-112 extractor drift: %v", err)
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply self-contained runtime backfill migration: %v", err)
	}

	var (
		hasRuntimeBlock bool
		staticVPNCore   bool
		load1           float64
		load5           float64
		load15          float64
		logicalCPUs     int
		runtimeTime     time.Time
		triggerPresent  bool
	)
	if err := pool.QueryRow(ctx, `
		SELECT
			capabilities ? 'runtimeMetrics',
			COALESCE((capabilities ->> 'vpnCore')::boolean, false),
			runtime_load_1,
			runtime_load_5,
			runtime_load_15,
			runtime_logical_cpus,
			runtime_collected_at,
			EXISTS (
				SELECT 1
				FROM pg_trigger
				WHERE tgrelid = 'agents'::regclass
				  AND tgname = 'agents_extract_runtime_metrics'
				  AND NOT tgisinternal
			)
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
		&triggerPresent,
	); err != nil {
		t.Fatalf("read repaired runtime metrics: %v", err)
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
	if version != "000130_multi_protocol_account_profiles" {
		t.Fatalf("applied schema version = %q, want 000130_multi_protocol_account_profiles", version)
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
			t.Fatalf("read migration %s: %v", base, err)
		}
		if err := os.WriteFile(filepath.Join(destination, base), content, 0o600); err != nil {
			t.Fatalf("copy migration %s: %v", base, err)
		}
	}
	return destination
}
