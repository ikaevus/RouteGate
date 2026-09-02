package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

func TestPlatformUpdateRolloutCreationIdempotency(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

	previousVersion := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	t.Cleanup(func() { buildinfo.Version = previousVersion })

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status, deployment_role)
		VALUES ('RG-96E3g idempotency fixture', 'active', 'vpn')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server fixture: %v", err)
	}

	repo := agents.NewRepository(pool)
	plan := agents.PlatformUpdateRolloutPlan{
		TargetVersion: "v1.2.3",
		Entries:       []agents.PlatformUpdateRolloutPlanEntry{{ServerID: serverID}},
	}
	requestHash := strings.Repeat("a", 64)
	key := "550e8400-e29b-41d4-a716-446655440010"

	rolloutID, replayed, err := repo.PersistPlatformUpdateRolloutPlanIdempotent(ctx, plan, key, requestHash)
	if err != nil || replayed {
		t.Fatalf("first create id=%q replayed=%t err=%v", rolloutID, replayed, err)
	}
	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin server-lock fixture: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, serverID); err != nil {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("hold server admission lock: %v", err)
	}
	replayCtx, cancelReplay := context.WithTimeout(ctx, 2*time.Second)
	replayedID, replayed, err := repo.PersistPlatformUpdateRolloutPlanIdempotent(replayCtx, plan, key, requestHash)
	cancelReplay()
	if err != nil || !replayed || replayedID != rolloutID {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("identical replay id=%q replayed=%t err=%v, want id=%q", replayedID, replayed, err, rolloutID)
	}
	conflictCtx, cancelConflict := context.WithTimeout(ctx, 2*time.Second)
	_, _, conflictErr := repo.PersistPlatformUpdateRolloutPlanIdempotent(conflictCtx, plan, key, strings.Repeat("b", 64))
	cancelConflict()
	if !errors.Is(conflictErr, agents.ErrPlatformUpdateRolloutIdempotencyConflict) {
		_ = lockTx.Rollback(ctx)
		t.Fatalf("conflicting replay error=%v, want idempotency conflict", conflictErr)
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release server admission lock: %v", err)
	}
	if _, _, err := repo.PersistPlatformUpdateRolloutPlanIdempotent(ctx, plan, key, strings.Repeat("b", 64)); !errors.Is(err, agents.ErrPlatformUpdateRolloutIdempotencyConflict) {
		t.Fatalf("conflicting replay error=%v, want idempotency conflict", err)
	}

	var rolloutCount, entryCount, jobCount int
	var storedKey, storedHash string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), min(creation_idempotency_key::text), min(creation_request_hash)
		FROM platform_update_rollouts
		WHERE creation_idempotency_key = $1::uuid
	`, key).Scan(&rolloutCount, &storedKey, &storedHash); err != nil {
		t.Fatalf("read creation evidence: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_update_rollout_entries WHERE rollout_id = $1::uuid`, rolloutID).Scan(&entryCount); err != nil {
		t.Fatalf("count rollout entries: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, serverID).Scan(&jobCount); err != nil {
		t.Fatalf("count mutation jobs: %v", err)
	}
	if rolloutCount != 1 || entryCount != 1 || jobCount != 0 || storedKey != key || storedHash != requestHash {
		t.Fatalf("evidence rollouts=%d entries=%d jobs=%d key=%q hash=%q", rolloutCount, entryCount, jobCount, storedKey, storedHash)
	}
	if _, err := pool.Exec(ctx, `UPDATE platform_update_rollouts SET creation_request_hash = $2 WHERE id = $1::uuid`, rolloutID, strings.Repeat("c", 64)); err == nil {
		t.Fatal("creation evidence remained mutable")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO platform_update_rollouts (target_version, creation_request_hash) VALUES ('v1.2.3', $1)`, requestHash); err == nil {
		t.Fatal("unpaired creation evidence was accepted")
	}

	concurrentKey := "550e8400-e29b-41d4-a716-446655440011"
	start := make(chan struct{})
	type result struct {
		id       string
		replayed bool
		err      error
	}
	results := make(chan result, 4)
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id, wasReplayed, createErr := repo.PersistPlatformUpdateRolloutPlanIdempotent(ctx, plan, concurrentKey, requestHash)
			results <- result{id: id, replayed: wasReplayed, err: createErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var concurrentID string
	freshCreates := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent create: %v", result.err)
		}
		if concurrentID == "" {
			concurrentID = result.id
		} else if result.id != concurrentID {
			t.Fatalf("concurrent creates returned ids %q and %q", concurrentID, result.id)
		}
		if !result.replayed {
			freshCreates++
		}
	}
	if freshCreates != 1 {
		t.Fatalf("concurrent fresh creates=%d, want 1", freshCreates)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_update_rollouts WHERE creation_idempotency_key = $1::uuid`, concurrentKey).Scan(&rolloutCount); err != nil {
		t.Fatalf("count concurrent rollouts: %v", err)
	}
	if rolloutCount != 1 {
		t.Fatalf("concurrent rollout count=%d, want 1", rolloutCount)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET status = 'failed', error_code = 'operator_stop',
		    started_at = clock_timestamp(), completed_at = clock_timestamp()
		WHERE id = $1::uuid
	`, rolloutID); err != nil {
		t.Fatalf("terminalize rollout fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET blocker_code = 'operator_stop'
		WHERE rollout_id = $1::uuid
	`, rolloutID); err != nil {
		t.Fatalf("set entry blocker fixture: %v", err)
	}
	view, err := repo.GetPlatformUpdateRollout(ctx, rolloutID)
	if err != nil {
		t.Fatalf("read bounded rollout view: %v", err)
	}
	if view.ErrorCode != "operator_stop" || len(view.Entries) != 1 || view.Entries[0].BlockerCode != "operator_stop" {
		t.Fatalf("durable reason view=%+v", view)
	}
}
