package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/configs"
)

func TestConfigApplyHistoryPaginationAndCleanupPreserveActiveJobs(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status)
		VALUES ('history-test', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}

	var versionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO config_versions (server_id, version, config_hash, status, rendered_config)
		VALUES ($1::uuid, 1, 'history-test-hash', 'validated', '{}'::jsonb)
		RETURNING id::text
	`, serverID).Scan(&versionID); err != nil {
		t.Fatalf("create config version: %v", err)
	}

	now := time.Now().UTC()
	statuses := []string{"succeeded", "failed", "pending", "in_progress"}
	for index, status := range statuses {
		createdAt := now.Add(time.Duration(index) * time.Minute)
		completedAt := any(nil)
		if status == "succeeded" || status == "failed" {
			completedAt = createdAt.Add(10 * time.Second)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO config_apply_jobs (
				server_id, config_version_id, action, status, request_payload, result_payload,
				created_at, updated_at, completed_at
			) VALUES (
				$1::uuid, $2::uuid, 'apply', $3, '{}'::jsonb, '{}'::jsonb,
				$4, $4, $5
			)
		`, serverID, versionID, status, createdAt, completedAt); err != nil {
			t.Fatalf("create %s job: %v", status, err)
		}
	}

	repository := configs.NewRepository(pool)
	items, total, err := repository.ListConfigApplyJobsPage(ctx, serverID, 2, 1)
	if err != nil {
		t.Fatalf("list paged history: %v", err)
	}
	if total != 4 {
		t.Fatalf("total=%d, want 4", total)
	}
	if len(items) != 2 {
		t.Fatalf("page length=%d, want 2", len(items))
	}
	if items[0].Status != "pending" || items[1].Status != "failed" {
		t.Fatalf("unexpected paged ordering: %q, %q", items[0].Status, items[1].Status)
	}

	deleted, err := repository.DeleteTerminalConfigApplyJobs(ctx, serverID)
	if err != nil {
		t.Fatalf("clear completed history: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d, want 2", deleted)
	}

	rows, err := pool.Query(ctx, `
		SELECT status
		FROM config_apply_jobs
		WHERE server_id = $1::uuid
		ORDER BY status
	`, serverID)
	if err != nil {
		t.Fatalf("list remaining jobs: %v", err)
	}
	defer rows.Close()

	remaining := make([]string, 0, 2)
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan remaining status: %v", err)
		}
		remaining = append(remaining, status)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read remaining statuses: %v", err)
	}
	if len(remaining) != 2 || remaining[0] != "in_progress" || remaining[1] != "pending" {
		t.Fatalf("remaining=%#v, want active jobs only", remaining)
	}
}
