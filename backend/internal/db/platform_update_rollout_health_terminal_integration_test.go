package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/jackc/pgx/v5"
)

func TestPlatformUpdateRolloutHealthTerminalStopSemantics(t *testing.T) {
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

	previousVersion := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	t.Cleanup(func() { buildinfo.Version = previousVersion })
	protocolVersion := buildinfo.AgentProtocolVersion
	now := time.Now().UTC()
	repo := agents.NewRepository(pool)

	createRollout := func(name string) (string, agents.PlatformUpdateJob, string) {
		t.Helper()
		var serverID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role)
			VALUES ($1, 'active', 'vpn')
			RETURNING id::text
		`, name).Scan(&serverID); err != nil {
			t.Fatalf("create server: %v", err)
		}
		if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
			ServerID: serverID, Hostname: name + "-agent", OS: "linux", Arch: "amd64",
			AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: name + "-token",
			Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
			Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{
				"schemaVersion": agents.PlatformUpdateCapabilitySchemaVersion,
				"state": agents.PlatformUpdateCapabilityStateReady,
				"request": agents.PlatformUpdateCapabilityRequestVersionOnly,
			}},
		}); err != nil {
			t.Fatalf("create ready Agent: %v", err)
		}
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3", Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: serverID}},
		})
		if err != nil {
			t.Fatalf("persist rollout: %v", err)
		}
		job, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit rollout mutation: %v", err)
		}
		return rolloutID, job, serverID
	}

	startJob := func(jobID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'in_progress', started_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("start update job: %v", err)
		}
	}
	dispatchJob := func(jobID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'mutation_dispatched', dispatched_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("dispatch update job: %v", err)
		}
	}

	t.Run("non-terminal job stays waiting without replacement", func(t *testing.T) {
		rolloutID, job, serverID := createRollout("RG-96E3e waiting")
		startJob(job.ID)
		dispatchJob(job.ID)
		result, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile non-terminal job: %v", err)
		}
		if result.RolloutStatus != "running" || result.EntryStatus != "updating" || result.WaitingReason != agents.PlatformUpdateRolloutHealthWaitingJob {
			t.Fatalf("waiting result = %+v", result)
		}
		var jobCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, serverID).Scan(&jobCount); err != nil {
			t.Fatalf("count waiting jobs: %v", err)
		}
		if jobCount != 1 {
			t.Fatalf("waiting reconciliation created %d jobs, want 1", jobCount)
		}
	})

	t.Run("failed bound job atomically stops rollout", func(t *testing.T) {
		rolloutID, job, serverID := createRollout("RG-96E3e failed")
		startJob(job.ID)
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'failed', error_code = 'test_failure', completed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, job.ID); err != nil {
			t.Fatalf("fail bound job: %v", err)
		}
		result, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile failed job: %v", err)
		}
		if result.RolloutStatus != "failed" || result.EntryStatus != "failed" {
			t.Fatalf("failed result = %+v", result)
		}
		assertRolloutHealthTerminalState(t, ctx, pool, rolloutID, serverID, "failed", agents.PlatformUpdateRolloutHealthNodeFailed)
	})

	t.Run("unknown bound outcome atomically stops rollout", func(t *testing.T) {
		rolloutID, job, serverID := createRollout("RG-96E3e unknown")
		startJob(job.ID)
		dispatchJob(job.ID)
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'outcome_unknown', error_code = 'test_unknown', completed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, job.ID); err != nil {
			t.Fatalf("mark bound job outcome unknown: %v", err)
		}
		result, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile unknown job: %v", err)
		}
		if result.RolloutStatus != "outcome_unknown" || result.EntryStatus != "outcome_unknown" {
			t.Fatalf("unknown result = %+v", result)
		}
		assertRolloutHealthTerminalState(t, ctx, pool, rolloutID, serverID, "outcome_unknown", agents.PlatformUpdateRolloutHealthOutcomeUnknown)
	})

	t.Run("terminal completion is wall-clock owned and immutable", func(t *testing.T) {
		const name = "RG-96E3e terminal provenance"
		_, job, _ := createRollout(name)
		startJob(job.ID)

		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire terminal transaction connection: %v", err)
		}
		defer conn.Release()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin terminal transaction: %v", err)
		}
		defer tx.Rollback(ctx)

		var transactionStartedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&transactionStartedAt); err != nil {
			t.Fatalf("read terminal transaction start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)

		agentVersion := "v1.2.3"
		if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
			TokenHash: name + "-token", AgentVersion: &agentVersion, ProtocolVersion: &protocolVersion,
		}); err != nil {
			t.Fatalf("record authenticated heartbeat after terminal transaction start: %v", err)
		}
		var heartbeatAt time.Time
		if err := pool.QueryRow(ctx, `
			SELECT last_authenticated_heartbeat_at
			FROM agents
			WHERE token_hash = $1
		`, name+"-token").Scan(&heartbeatAt); err != nil {
			t.Fatalf("read authenticated heartbeat proof: %v", err)
		}
		if !heartbeatAt.After(transactionStartedAt) {
			t.Fatalf("heartbeat %s is not after terminal transaction start %s", heartbeatAt, transactionStartedAt)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'succeeded',
				dispatched_at = COALESCE(dispatched_at, now()),
				completed_at = now(),
				updated_at = now()
			WHERE id = $1::uuid
		`, job.ID); err != nil {
			t.Fatalf("terminalize update job: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit terminal update job: %v", err)
		}

		var completedAt time.Time
		if err := pool.QueryRow(ctx, `
			SELECT completed_at
			FROM agent_platform_update_jobs
			WHERE id = $1::uuid
		`, job.ID).Scan(&completedAt); err != nil {
			t.Fatalf("read database-owned completion timestamp: %v", err)
		}
		if !completedAt.After(heartbeatAt) {
			t.Fatalf("terminal completion %s must be after intervening heartbeat %s", completedAt, heartbeatAt)
		}

		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET completed_at = completed_at - interval '1 second'
			WHERE id = $1::uuid
		`, job.ID); err == nil {
			t.Fatal("terminal completion timestamp rewrite must be rejected")
		}
		var completedAfterRewrite time.Time
		if err := pool.QueryRow(ctx, `
			SELECT completed_at
			FROM agent_platform_update_jobs
			WHERE id = $1::uuid
		`, job.ID).Scan(&completedAfterRewrite); err != nil {
			t.Fatalf("read completion timestamp after rejected rewrite: %v", err)
		}
		if !completedAfterRewrite.Equal(completedAt) {
			t.Fatalf("terminal completion changed from %s to %s after rejected rewrite", completedAt, completedAfterRewrite)
		}
	})
}

func assertRolloutHealthTerminalState(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgxRow
}, rolloutID, serverID, wantStatus, wantCode string) {
	t.Helper()
	var rolloutStatus, errorCode, entryStatus string
	var jobCount int
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_code, '')
		FROM platform_update_rollouts WHERE id = $1::uuid
	`, rolloutID).Scan(&rolloutStatus, &errorCode); err != nil {
		t.Fatalf("read terminal rollout: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT status FROM platform_update_rollout_entries WHERE rollout_id = $1::uuid
	`, rolloutID).Scan(&entryStatus); err != nil {
		t.Fatalf("read terminal entry: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, serverID).Scan(&jobCount); err != nil {
		t.Fatalf("count terminal jobs: %v", err)
	}
	if rolloutStatus != wantStatus || entryStatus != wantStatus || errorCode != wantCode || jobCount != 1 {
		t.Fatalf("terminal state rollout=%q entry=%q code=%q jobs=%d", rolloutStatus, entryStatus, errorCode, jobCount)
	}
}

type pgxRow = pgx.Row
