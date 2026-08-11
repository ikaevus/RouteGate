package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
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

	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}
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
	if version != "000113_delivery_foundation" {
		t.Fatalf("applied schema version = %q, want 000113_delivery_foundation", version)
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
		"message_body":           {},
		"html_body":              {},
		"vless_uri":              {},
		"vless_link":             {},
		"connect_url":            {},
		"qr_payload":             {},
		"qr_image":               {},
		"credentials":            {},
		"provider_raw_response":  {},
		"provider_raw_error":     {},
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
