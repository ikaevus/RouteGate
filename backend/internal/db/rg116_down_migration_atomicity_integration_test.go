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

// TestRG116DownMigrationHistoryAtomicity protects the production-like rollback
// contract introduced after the RG-116 deployment incident: removing the
// physical schema introduced by a migration and removing that migration's
// schema_migrations row must happen in the same PostgreSQL transaction.
//
// The test injects a database trigger that deliberately rejects deletion of
// the migration-history row. If transaction ownership is correct, the prior
// DDL in the down migration is rolled back as well.
func TestRG116DownMigrationHistoryAtomicity(t *testing.T) {
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

	t.Run("000151 rolls DDL back when history deletion fails", func(t *testing.T) {
		resetPublicSchema(t, ctx, pool)
		if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
			t.Fatalf("apply current migrations: %v", err)
		}

		installHistoryDeleteBlocker(t, ctx, pool, "000151_delivery_token_generation")
		downSQL := readMigrationSQL(t, "000151_delivery_token_generation.down.sql")
		if _, err := pool.Exec(ctx, downSQL); err == nil {
			t.Fatal("expected 000151 down migration to fail when history deletion is rejected")
		}

		if !columnExists(t, ctx, pool, "deliveries", "subscription_token_id") {
			t.Fatal("deliveries.subscription_token_id disappeared despite rollback; DDL/history were not atomic")
		}
		if !migrationRecorded(t, ctx, pool, "000151_delivery_token_generation") {
			t.Fatal("000151 history row disappeared despite rollback")
		}
	})

	t.Run("000150 rolls DDL back when history deletion fails", func(t *testing.T) {
		resetPublicSchema(t, ctx, pool)
		if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
			t.Fatalf("apply current migrations: %v", err)
		}

		// Reverse migration order first: 151 is layered on top of 150.
		if _, err := pool.Exec(ctx, readMigrationSQL(t, "000151_delivery_token_generation.down.sql")); err != nil {
			t.Fatalf("roll back 000151 prerequisite: %v", err)
		}

		installHistoryDeleteBlocker(t, ctx, pool, "000150_delivery_device_scope")
		downSQL := readMigrationSQL(t, "000150_delivery_device_scope.down.sql")
		if _, err := pool.Exec(ctx, downSQL); err == nil {
			t.Fatal("expected 000150 down migration to fail when history deletion is rejected")
		}

		if !columnExists(t, ctx, pool, "deliveries", "device_id") {
			t.Fatal("deliveries.device_id disappeared despite rollback; DDL/history were not atomic")
		}
		if !migrationRecorded(t, ctx, pool, "000150_delivery_device_scope") {
			t.Fatal("000150 history row disappeared despite rollback")
		}
	})
}

func readMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(content)
}

func installHistoryDeleteBlocker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION rg116_reject_history_delete()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.version = $1 THEN
				RAISE EXCEPTION 'injected migration-history delete failure';
			END IF;
			RETURN OLD;
		END;
		$$;
	`, version); err == nil {
		// PostgreSQL function bodies cannot bind $1 this way; create the
		// version-specific function below instead.
		_, _ = pool.Exec(ctx, `DROP FUNCTION IF EXISTS rg116_reject_history_delete()`)
	}

	// version is constrained to checked-in migration identifiers supplied by
	// the test itself; format it as a SQL literal without exposing production
	// input to dynamic SQL.
	functionSQL := `
		CREATE OR REPLACE FUNCTION rg116_reject_history_delete()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.version = '` + version + `' THEN
				RAISE EXCEPTION 'injected migration-history delete failure';
			END IF;
			RETURN OLD;
		END;
		$$;
		DROP TRIGGER IF EXISTS rg116_reject_history_delete_trigger ON schema_migrations;
		CREATE TRIGGER rg116_reject_history_delete_trigger
		BEFORE DELETE ON schema_migrations
		FOR EACH ROW EXECUTE FUNCTION rg116_reject_history_delete();
	`
	if _, err := pool.Exec(ctx, functionSQL); err != nil {
		t.Fatalf("install migration-history delete blocker: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS rg116_reject_history_delete_trigger ON schema_migrations`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS rg116_reject_history_delete()`)
	})
}

func columnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName, columnName string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists); err != nil {
		t.Fatalf("check %s.%s existence: %v", tableName, columnName, err)
	}
	return exists
}

func migrationRecorded(t *testing.T, ctx context.Context, pool *pgxpool.Pool, version string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
		t.Fatalf("check schema_migrations %s: %v", version, err)
	}
	return exists
}
