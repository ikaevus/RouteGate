package db

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestPlatformUpdateRolloutPersistenceSerializesConcurrentAdmissionAndStop(t *testing.T) {
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

	type nodeFixture struct {
		serverID string
		agentID  string
		jobID    string
	}
	createNode := func(name string) nodeFixture {
		t.Helper()
		var node nodeFixture
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role)
			VALUES ($1, 'active', 'vpn')
			RETURNING id::text
		`, name).Scan(&node.serverID); err != nil {
			t.Fatalf("create server %s: %v", name, err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO agents (
				server_id, hostname, os, arch, agent_version, protocol_version,
				status, token_hash, capabilities, registered_at, last_seen_at
			) VALUES (
				$1::uuid, $2, 'linux', 'amd64', 'test', 1,
				'online', $3, '{}'::jsonb, now(), now()
			)
			RETURNING id::text
		`, node.serverID, name, name+"-token").Scan(&node.agentID); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
			VALUES ($1::uuid, $2::uuid, 'v1.2.3')
			RETURNING id::text
		`, node.serverID, node.agentID).Scan(&node.jobID); err != nil {
			t.Fatalf("create update job %s: %v", name, err)
		}
		return node
	}

	createRollout := func(name string, running bool) (string, nodeFixture) {
		t.Helper()
		node := createNode(name)
		var rolloutID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO platform_update_rollouts (target_version)
			VALUES ('v1.2.3')
			RETURNING id::text
		`).Scan(&rolloutID); err != nil {
			t.Fatalf("create rollout %s: %v", name, err)
		}
		if running {
			if _, err := pool.Exec(ctx, `
				UPDATE platform_update_rollouts
				SET status = 'running', started_at = now()
				WHERE id = $1::uuid
			`, rolloutID); err != nil {
				t.Fatalf("start rollout %s: %v", name, err)
			}
		}
		return rolloutID, node
	}

	assertBlocked := func(label string, result <-chan error) {
		t.Helper()
		select {
		case err := <-result:
			t.Fatalf("%s completed before the parent-row lock was released: %v", label, err)
		case <-time.After(150 * time.Millisecond):
		}
	}
	awaitResult := func(label string, result <-chan error) error {
		t.Helper()
		select {
		case err := <-result:
			return err
		case <-time.After(5 * time.Second):
			t.Fatalf("%s remained blocked after the parent-row lock was released", label)
			return nil
		}
	}

	t.Run("membership commit before start admits the member then start succeeds", func(t *testing.T) {
		rolloutID, node := createRollout("membership-first", false)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin membership transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `
			INSERT INTO platform_update_rollout_entries (rollout_id, server_id, target_version, position)
			VALUES ($1::uuid, $2::uuid, 'v1.2.3', 0)
		`, rolloutID, node.serverID); err != nil {
			t.Fatalf("admit member while pending: %v", err)
		}

		startResult := make(chan error, 1)
		go func() {
			_, err := pool.Exec(ctx, `
				UPDATE platform_update_rollouts
				SET status = 'running', started_at = now()
				WHERE id = $1::uuid
			`, rolloutID)
			startResult <- err
		}()
		assertBlocked("concurrent rollout start", startResult)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit membership: %v", err)
		}
		if err := awaitResult("concurrent rollout start", startResult); err != nil {
			t.Fatalf("start after membership commit: %v", err)
		}

		var status string
		var memberCount int
		if err := pool.QueryRow(ctx, `SELECT status FROM platform_update_rollouts WHERE id = $1::uuid`, rolloutID).Scan(&status); err != nil {
			t.Fatalf("read rollout status: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_update_rollout_entries WHERE rollout_id = $1::uuid`, rolloutID).Scan(&memberCount); err != nil {
			t.Fatalf("count rollout members: %v", err)
		}
		if status != "running" || memberCount != 1 {
			t.Fatalf("membership-first result status=%q members=%d want running/1", status, memberCount)
		}
	})

	t.Run("start commit before membership rejects the late member", func(t *testing.T) {
		rolloutID, node := createRollout("start-first", false)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin start transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `
			UPDATE platform_update_rollouts
			SET status = 'running', started_at = now()
			WHERE id = $1::uuid
		`, rolloutID); err != nil {
			t.Fatalf("stage rollout start: %v", err)
		}

		membershipResult := make(chan error, 1)
		go func() {
			_, err := pool.Exec(ctx, `
				INSERT INTO platform_update_rollout_entries (rollout_id, server_id, target_version, position)
				VALUES ($1::uuid, $2::uuid, 'v1.2.3', 0)
			`, rolloutID, node.serverID)
			membershipResult <- err
		}()
		assertBlocked("concurrent membership admission", membershipResult)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit rollout start: %v", err)
		}
		if err := awaitResult("concurrent membership admission", membershipResult); err == nil {
			t.Fatal("late membership was admitted after concurrent rollout start committed")
		}
	})

	t.Run("update admission commit before stop wins and stop is rejected", func(t *testing.T) {
		rolloutID, node := createRollout("admission-first", false)
		var entryID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO platform_update_rollout_entries (rollout_id, server_id, target_version, position)
			VALUES ($1::uuid, $2::uuid, 'v1.2.3', 0)
			RETURNING id::text
		`, rolloutID, node.serverID).Scan(&entryID); err != nil {
			t.Fatalf("create queued entry: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE platform_update_rollouts
			SET status = 'running', started_at = now()
			WHERE id = $1::uuid
		`, rolloutID); err != nil {
			t.Fatalf("start rollout: %v", err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin update-admission transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `
			UPDATE platform_update_rollout_entries
			SET platform_update_job_id = $2::uuid, status = 'updating'
			WHERE id = $1::uuid
		`, entryID, node.jobID); err != nil {
			t.Fatalf("stage update admission: %v", err)
		}

		stopResult := make(chan error, 1)
		go func() {
			_, err := pool.Exec(ctx, `
				UPDATE platform_update_rollouts
				SET status = 'failed', error_code = 'concurrent_stop', completed_at = now()
				WHERE id = $1::uuid
			`, rolloutID)
			stopResult <- err
		}()
		assertBlocked("concurrent rollout stop", stopResult)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit update admission: %v", err)
		}
		if err := awaitResult("concurrent rollout stop", stopResult); err == nil {
			t.Fatal("rollout stop succeeded after concurrent update admission committed")
		}

		var rolloutStatus, entryStatus string
		if err := pool.QueryRow(ctx, `SELECT status FROM platform_update_rollouts WHERE id = $1::uuid`, rolloutID).Scan(&rolloutStatus); err != nil {
			t.Fatalf("read rollout status: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT status FROM platform_update_rollout_entries WHERE id = $1::uuid`, entryID).Scan(&entryStatus); err != nil {
			t.Fatalf("read entry status: %v", err)
		}
		if rolloutStatus != "running" || entryStatus != "updating" {
			t.Fatalf("admission-first result rollout=%q entry=%q want running/updating", rolloutStatus, entryStatus)
		}
	})

	t.Run("stop commit before update admission wins and admission is rejected", func(t *testing.T) {
		rolloutID, node := createRollout("stop-first", false)
		var entryID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO platform_update_rollout_entries (rollout_id, server_id, target_version, position)
			VALUES ($1::uuid, $2::uuid, 'v1.2.3', 0)
			RETURNING id::text
		`, rolloutID, node.serverID).Scan(&entryID); err != nil {
			t.Fatalf("create queued entry: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE platform_update_rollouts
			SET status = 'running', started_at = now()
			WHERE id = $1::uuid
		`, rolloutID); err != nil {
			t.Fatalf("start rollout: %v", err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin stop transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `
			UPDATE platform_update_rollouts
			SET status = 'failed', error_code = 'concurrent_stop', completed_at = now()
			WHERE id = $1::uuid
		`, rolloutID); err != nil {
			t.Fatalf("stage rollout stop: %v", err)
		}

		admissionResult := make(chan error, 1)
		go func() {
			_, err := pool.Exec(ctx, `
				UPDATE platform_update_rollout_entries
				SET platform_update_job_id = $2::uuid, status = 'updating'
				WHERE id = $1::uuid
			`, entryID, node.jobID)
			admissionResult <- err
		}()
		assertBlocked("concurrent update admission", admissionResult)
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit rollout stop: %v", err)
		}
		if err := awaitResult("concurrent update admission", admissionResult); err == nil {
			t.Fatal("update admission succeeded after concurrent rollout stop committed")
		}

		var rolloutStatus, entryStatus string
		if err := pool.QueryRow(ctx, `SELECT status FROM platform_update_rollouts WHERE id = $1::uuid`, rolloutID).Scan(&rolloutStatus); err != nil {
			t.Fatalf("read rollout status: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT status FROM platform_update_rollout_entries WHERE id = $1::uuid`, entryID).Scan(&entryStatus); err != nil {
			t.Fatalf("read entry status: %v", err)
		}
		if rolloutStatus != "failed" || entryStatus != "queued" {
			t.Fatalf("stop-first result rollout=%q entry=%q want failed/queued", rolloutStatus, entryStatus)
		}
	})

	_ = fmt.Sprintf // keep fmt import available for future concurrency diagnostics without changing test semantics
}
