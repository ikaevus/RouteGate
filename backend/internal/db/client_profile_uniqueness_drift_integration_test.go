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
)

func TestClientProfileSchemaInvariantRepairRepairsAlreadyAppliedHistoricalDrift(t *testing.T) {
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
	preRepairDir := copyClientProfileMigrationsBefore(t, "../../migrations", "000130z_client_profile_schema_invariant_repair.up.sql")
	if err := Migrate(ctx, pool, preRepairDir, logger); err != nil {
		t.Fatalf("apply pre-repair migrations: %v", err)
	}

	var serverID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status)
		VALUES ('Client profile drift server', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create drift server: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('client-profile-drift', 'sing-box', 'Client profile drift', 'active', $1::uuid)
		RETURNING id::text
	`, serverID).Scan(&accountID); err != nil {
		t.Fatalf("create drift VPN account: %v", err)
	}

	if _, err := pool.Exec(ctx, `DROP INDEX IF EXISTS idx_vpn_client_profiles_account_format_active`); err != nil {
		t.Fatalf("drop canonical active profile index: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id, format, name, config, is_active, updated_at)
		VALUES
			($1::uuid, 'sing-box', 'Older profile', '{}'::jsonb, TRUE, now() - interval '2 hours'),
			($1::uuid, 'sing-box', 'Newest profile', '{}'::jsonb, TRUE, now() - interval '1 hour')
	`, accountID); err != nil {
		t.Fatalf("insert historical duplicate profiles: %v", err)
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply current migrations with invariant repair: %v", err)
	}

	var activeCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM vpn_client_profiles
		WHERE vpn_account_id = $1::uuid
		  AND format = 'sing-box'
		  AND is_active
	`, accountID).Scan(&activeCount); err != nil {
		t.Fatalf("count repaired active profiles: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active client profiles after repair = %d, want 1", activeCount)
	}

	var activeName string
	if err := pool.QueryRow(ctx, `
		SELECT name
		FROM vpn_client_profiles
		WHERE vpn_account_id = $1::uuid
		  AND format = 'sing-box'
		  AND is_active
	`, accountID).Scan(&activeName); err != nil {
		t.Fatalf("read repaired active profile: %v", err)
	}
	if activeName != "Newest profile" {
		t.Fatalf("active repaired profile = %q, want Newest profile", activeName)
	}

	var indexPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename = 'vpn_client_profiles'
			  AND indexname = 'idx_vpn_client_profiles_account_format_active'
		)
	`).Scan(&indexPresent); err != nil {
		t.Fatalf("check canonical active profile index: %v", err)
	}
	if !indexPresent {
		t.Fatal("canonical active client-profile uniqueness index was not restored")
	}

	var upsertedName string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id, format, name, config, is_active)
		VALUES ($1::uuid, 'sing-box', 'Upserted profile', '{}'::jsonb, TRUE)
		ON CONFLICT (vpn_account_id, format) WHERE is_active
		DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		RETURNING name
	`, accountID).Scan(&upsertedName); err != nil {
		t.Fatalf("ON CONFLICT client-profile upsert after repair: %v", err)
	}
	if upsertedName != "Upserted profile" {
		t.Fatalf("upsert returned profile = %q, want Upserted profile", upsertedName)
	}

	var repairApplied bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM schema_migrations
			WHERE version = '000130z_client_profile_schema_invariant_repair'
		)
	`).Scan(&repairApplied); err != nil {
		t.Fatalf("check invariant repair migration record: %v", err)
	}
	if !repairApplied {
		t.Fatal("client-profile invariant repair migration was not recorded")
	}

	version, err := NewSchemaVersionRepository(pool).AppliedSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read applied schema version: %v", err)
	}
	if version != "000140_platform_update_unknown_outcome_interlock" {
		t.Fatalf("applied schema version = %q, want 000140_platform_update_unknown_outcome_interlock", version)
	}
}

func copyClientProfileMigrationsBefore(t *testing.T, sourceDir, stopBefore string) string {
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
