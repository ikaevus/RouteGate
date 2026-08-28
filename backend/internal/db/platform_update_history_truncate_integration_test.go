package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlatformUpdateDurableHistoryRejectsTruncate(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	for _, tc := range []struct {
		name        string
		triggerName string
		tableName   string
		statement   string
	}{
		{
			name:        "single-node update jobs",
			triggerName: "trg_agent_platform_update_jobs_no_truncate",
			tableName:   "agent_platform_update_jobs",
			statement:   "TRUNCATE TABLE agent_platform_update_jobs CASCADE",
		},
		{
			name:        "rollouts",
			triggerName: "trg_platform_update_rollouts_no_truncate",
			tableName:   "platform_update_rollouts",
			statement:   "TRUNCATE TABLE platform_update_rollouts CASCADE",
		},
		{
			name:        "rollout entries",
			triggerName: "trg_platform_update_rollout_entries_no_truncate",
			tableName:   "platform_update_rollout_entries",
			statement:   "TRUNCATE TABLE platform_update_rollout_entries",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var installed bool
			if err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM pg_trigger
					WHERE tgrelid = $1::regclass
					  AND tgname = $2
					  AND NOT tgisinternal
				)
			`, tc.tableName, tc.triggerName).Scan(&installed); err != nil {
				t.Fatalf("inspect truncate guard: %v", err)
			}
			if !installed {
				t.Fatalf("truncate guard %s is not installed on %s", tc.triggerName, tc.tableName)
			}

			_, err := pool.Exec(ctx, tc.statement)
			if err == nil {
				t.Fatalf("%s history unexpectedly allowed TRUNCATE", tc.name)
			}
			if !strings.Contains(err.Error(), "platform update durable history cannot be truncated") {
				t.Fatalf("unexpected TRUNCATE rejection for %s: %v", tc.name, err)
			}
		})
	}
}
