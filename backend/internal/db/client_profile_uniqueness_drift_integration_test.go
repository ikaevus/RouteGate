package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestClientProfileUniquenessRepairMigrationRepairsHistoricalDrift(t *testing.T) {
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
	migrationsBeforeRepair := copyMigrationsBefore(
		t,
		"../../migrations",
		"000117_vpn_client_profile_uniqueness_repair.up.sql",
	)
	if err := Migrate(ctx, pool, migrationsBeforeRepair, logger); err != nil {
		t.Fatalf("apply migrations before uniqueness repair: %v", err)
	}

	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status)
		VALUES ('rg112d-drift-fixture', 'sing-box', 'RG-112D drift fixture', 'active')
		RETURNING id::text
	`).Scan(&accountID); err != nil {
		t.Fatalf("create drift fixture account: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		ALTER TABLE vpn_client_profiles
		DROP CONSTRAINT vpn_client_profiles_vpn_account_id_key
	`); err != nil {
		t.Fatalf("remove historical client-profile uniqueness constraint: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_client_profiles (
			vpn_account_id, name, created_at, updated_at
		) VALUES
			($1::uuid, 'Older profile', now() - interval '2 hours', now() - interval '1 hour'),
			($1::uuid, 'Newest profile', now() - interval '1 hour', now())
	`, accountID); err != nil {
		t.Fatalf("create duplicate historical client profiles: %v", err)
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply uniqueness repair migration: %v", err)
	}

	var profileCount int
	var preservedName string
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), MAX(name)
		FROM vpn_client_profiles
		WHERE vpn_account_id = $1::uuid
	`, accountID).Scan(&profileCount, &preservedName); err != nil {
		t.Fatalf("read repaired client profiles: %v", err)
	}
	if profileCount != 1 {
		t.Fatalf("client profile count = %d, want 1", profileCount)
	}
	if preservedName != "Newest profile" {
		t.Fatalf("preserved profile = %q, want Newest profile", preservedName)
	}

	var uniqueIndexPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index AS index_row
			JOIN pg_attribute AS attribute_row
			  ON attribute_row.attrelid = index_row.indrelid
			 AND attribute_row.attnum = index_row.indkey[0]
			WHERE index_row.indrelid = 'vpn_client_profiles'::regclass
			  AND index_row.indisunique
			  AND index_row.indisvalid
			  AND index_row.indpred IS NULL
			  AND index_row.indexprs IS NULL
			  AND index_row.indnkeyatts = 1
			  AND attribute_row.attname = 'vpn_account_id'
		)
	`).Scan(&uniqueIndexPresent); err != nil {
		t.Fatalf("check repaired uniqueness: %v", err)
	}
	if !uniqueIndexPresent {
		t.Fatal("vpn_client_profiles.vpn_account_id uniqueness was not restored")
	}

	var upsertedName string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id, name)
		VALUES ($1::uuid, 'Should not replace preserved profile')
		ON CONFLICT (vpn_account_id) DO UPDATE
		SET vpn_account_id = EXCLUDED.vpn_account_id
		RETURNING name
	`, accountID).Scan(&upsertedName); err != nil {
		t.Fatalf("ON CONFLICT client-profile upsert after repair: %v", err)
	}
	if upsertedName != "Newest profile" {
		t.Fatalf("upsert returned profile = %q, want Newest profile", upsertedName)
	}

	version, err := NewSchemaVersionRepository(pool).AppliedSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("read applied schema version: %v", err)
	}
	if version != "000120_observability_alert_recovery" {
		t.Fatalf("applied schema version = %q, want 000120_observability_alert_recovery", version)
	}
}
