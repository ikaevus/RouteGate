package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestUpdateJobOperationStagePairingConstraint(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `
		INSERT INTO update_jobs (operation, status, stage)
		VALUES ('discovery', 'pending', 'discovery')
	`); err != nil {
		t.Fatalf("insert valid discovery job: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO update_jobs (operation, status, stage)
		VALUES ('stage', 'pending', 'stage')
	`); err != nil {
		t.Fatalf("insert valid stage job: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO update_jobs (operation, status, stage)
		VALUES ('preflight', 'pending', 'discovery')
	`); err == nil {
		t.Fatal("mismatched update job operation/stage was accepted")
	}
}
