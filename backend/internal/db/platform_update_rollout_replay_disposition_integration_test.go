package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

func TestPlatformUpdateRolloutReplayDispositionDeterministic(t *testing.T) {
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
	protocolVersion := buildinfo.AgentProtocolVersion
	now := time.Now().UTC()
	repo := agents.NewRepository(pool)

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status, deployment_role)
		VALUES ('RG-96E3f deterministic replay', 'active', 'vpn')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "rg-96e3f-replay-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: "rg-96e3f-replay-token",
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
		TargetVersion: "v1.2.3",
		Entries:       []agents.PlatformUpdateRolloutPlanEntry{{ServerID: serverID}},
	})
	if err != nil {
		t.Fatalf("persist rollout: %v", err)
	}

	// Hold E3d's canonical global admission mutex while both E3f invocations
	// complete their initial inspection and block at admission. Releasing it then
	// forces one fresh admission followed by one exact bound-job replay.
	gateTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin admission gate: %v", err)
	}
	defer gateTx.Rollback(ctx)
	if _, err := gateTx.Exec(ctx, `SELECT lock_platform_update_admission_global()`); err != nil {
		t.Fatalf("hold admission gate: %v", err)
	}

	type stepCall struct {
		result agents.PlatformUpdateRolloutStepResult
		err    error
	}
	start := make(chan struct{})
	calls := make(chan stepCall, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
			calls <- stepCall{result: result, err: err}
		}()
	}
	close(start)

	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		var blockedAdmissions int
		if err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND pid <> pg_backend_pid()
			  AND state = 'active'
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%lock_platform_update_admission_global()%'
		`).Scan(&blockedAdmissions); err != nil {
			t.Fatalf("observe blocked admissions: %v", err)
		}
		if blockedAdmissions >= 2 {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatalf("only %d E3f calls reached the admission mutex", blockedAdmissions)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := gateTx.Commit(ctx); err != nil {
		t.Fatalf("release admission gate: %v", err)
	}
	wg.Wait()
	close(calls)

	var jobID string
	var admittedCount, replayCount int
	for call := range calls {
		if call.err != nil {
			t.Fatalf("concurrent step error: %v", call.err)
		}
		if call.result.ServerID != serverID || call.result.JobID == "" {
			t.Fatalf("concurrent step identity = %+v, want server %q with bound job", call.result, serverID)
		}
		if jobID == "" {
			jobID = call.result.JobID
		} else if call.result.JobID != jobID {
			t.Fatalf("concurrent calls observed different jobs %q and %q", jobID, call.result.JobID)
		}
		switch call.result.Action {
		case agents.PlatformUpdateRolloutStepMutationAdmitted:
			admittedCount++
		case agents.PlatformUpdateRolloutStepMutationInProgress:
			replayCount++
		default:
			t.Fatalf("concurrent step action = %q, want admitted or exact replay: %+v", call.result.Action, call.result)
		}
	}
	if admittedCount != 1 || replayCount != 1 {
		t.Fatalf("dispositions admitted=%d replay=%d, want exactly one of each", admittedCount, replayCount)
	}

	var jobCount, updatingCount int
	var boundJobID string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, serverID).Scan(&jobCount); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*), max(platform_update_job_id::text)
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid AND status = 'updating'
	`, rolloutID).Scan(&updatingCount, &boundJobID); err != nil {
		t.Fatalf("read updating entry: %v", err)
	}
	if jobCount != 1 || updatingCount != 1 || boundJobID != jobID {
		t.Fatalf("after replay jobs=%d updating=%d bound=%q want job=%q", jobCount, updatingCount, boundJobID, jobID)
	}
}
