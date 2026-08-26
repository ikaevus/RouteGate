package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestPlatformUpdateRolloutPersistenceEnforcesStopAndJobIdentity(t *testing.T) {
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

	serverIDs := make([]string, 2)
	agentIDs := make([]string, 2)
	for index, name := range []string{"rollout-node-a", "rollout-node-b"} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role)
			VALUES ($1, 'active', 'vpn')
			RETURNING id::text
		`, name).Scan(&serverIDs[index]); err != nil {
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
		`, serverIDs[index], name, name+"-token").Scan(&agentIDs[index]); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
	}

	createJob := func(serverID, agentID, version string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
			VALUES ($1::uuid, $2::uuid, $3)
			RETURNING id::text
		`, serverID, agentID, version).Scan(&id); err != nil {
			t.Fatalf("create platform update job: %v", err)
		}
		return id
	}

	matchingJobID := createJob(serverIDs[0], agentIDs[0], "v1.2.3")
	otherServerJobID := createJob(serverIDs[1], agentIDs[1], "v1.2.3")

	var rolloutID, entryID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO platform_update_rollouts (target_version)
		VALUES ('v1.2.3')
		RETURNING id::text
	`).Scan(&rolloutID); err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO platform_update_rollout_entries (rollout_id, server_id, target_version, position)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3', 0)
		RETURNING id::text
	`, rolloutID, serverIDs[0]).Scan(&entryID); err != nil {
		t.Fatalf("create rollout entry: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET platform_update_job_id = $2::uuid, status = 'updating'
		WHERE id = $1::uuid
	`, entryID, otherServerJobID); err == nil {
		t.Fatal("rollout entry accepted an update job belonging to another server")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET platform_update_job_id = $2::uuid, status = 'updating'
		WHERE id = $1::uuid
	`, entryID, matchingJobID); err != nil {
		t.Fatalf("bind matching update job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET status = 'failed', blocker_code = 'update_failed', completed_at = now()
		WHERE id = $1::uuid
	`, entryID); err != nil {
		t.Fatalf("terminalize rollout entry: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET status = 'updating', blocker_code = NULL, completed_at = NULL
		WHERE id = $1::uuid
	`, entryID); err == nil {
		t.Fatal("terminal rollout entry was resurrected to updating")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET platform_update_job_id = NULL
		WHERE id = $1::uuid
	`, entryID); err == nil {
		t.Fatal("bound rollout entry update job identity was cleared")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET status = 'running', started_at = now()
		WHERE id = $1::uuid
	`, rolloutID); err != nil {
		t.Fatalf("start rollout: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET status = 'failed', error_code = 'entry_failed', completed_at = now()
		WHERE id = $1::uuid
	`, rolloutID); err != nil {
		t.Fatalf("terminalize rollout: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET status = 'running', error_code = NULL, completed_at = NULL
		WHERE id = $1::uuid
	`, rolloutID); err == nil {
		t.Fatal("terminal rollout was resurrected to running")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET target_version = 'v1.2.4'
		WHERE id = $1::uuid
	`, rolloutID); err == nil {
		t.Fatal("rollout target version was mutated after creation")
	}
}
