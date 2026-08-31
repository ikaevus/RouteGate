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

func TestPlatformUpdateRolloutStepBoundary(t *testing.T) {
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

	type readyNode struct {
		serverID string
		token    string
	}
	createReadyNode := func(name string) readyNode {
		t.Helper()
		var serverID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role)
			VALUES ($1, 'active', 'vpn')
			RETURNING id::text
		`, name).Scan(&serverID); err != nil {
			t.Fatalf("create server %s: %v", name, err)
		}
		token := name + "-token"
		if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
			ServerID: serverID, Hostname: name + "-agent", OS: "linux", Arch: "amd64",
			AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: token,
			Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
			Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{
				"schemaVersion": agents.PlatformUpdateCapabilitySchemaVersion,
				"state": agents.PlatformUpdateCapabilityStateReady,
				"request": agents.PlatformUpdateCapabilityRequestVersionOnly,
			}},
		}); err != nil {
			t.Fatalf("create ready Agent %s: %v", name, err)
		}
		return readyNode{serverID: serverID, token: token}
	}

	succeedJob := func(jobID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'in_progress', started_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("start job %s: %v", jobID, err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'mutation_dispatched', dispatched_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("dispatch job %s: %v", jobID, err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'succeeded', completed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("succeed job %s: %v", jobID, err)
		}
	}

	terminalizeJob := func(jobID, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'in_progress', started_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("start terminal job %s: %v", jobID, err)
		}
		if status == "outcome_unknown" {
			if _, err := pool.Exec(ctx, `
				UPDATE agent_platform_update_jobs
				SET status = 'mutation_dispatched', dispatched_at = clock_timestamp(), updated_at = clock_timestamp()
				WHERE id = $1::uuid
			`, jobID); err != nil {
				t.Fatalf("dispatch outcome-unknown job %s: %v", jobID, err)
			}
		}
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = $2, completed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID, status); err != nil {
			t.Fatalf("terminalize job %s as %s: %v", jobID, status, err)
		}
	}

	heartbeat := func(node readyNode) {
		t.Helper()
		version := "v1.2.3"
		if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
			TokenHash: node.token, AgentVersion: &version, ProtocolVersion: &protocolVersion,
		}); err != nil {
			t.Fatalf("heartbeat %s: %v", node.serverID, err)
		}
	}

	t.Run("concurrent replay admits one job and health proof returns before next node", func(t *testing.T) {
		first := createReadyNode("RG-96E3f first")
		second := createReadyNode("RG-96E3f second")
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3",
			Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: first.serverID}, {ServerID: second.serverID}},
		})
		if err != nil {
			t.Fatalf("persist rollout: %v", err)
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
		wg.Wait()
		close(calls)

		var jobID string
		for call := range calls {
			if call.err != nil {
				t.Fatalf("concurrent step error: %v", call.err)
			}
			if call.result.ServerID != first.serverID {
				t.Fatalf("concurrent step server = %q, want %q: %+v", call.result.ServerID, first.serverID, call.result)
			}
			if call.result.JobID == "" {
				t.Fatalf("concurrent step omitted bound job identity: %+v", call.result)
			}
			if jobID == "" {
				jobID = call.result.JobID
			} else if call.result.JobID != jobID {
				t.Fatalf("concurrent calls observed different jobs %q and %q", jobID, call.result.JobID)
			}
		}

		var jobCount, updatingCount int
		var boundJobID string
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, first.serverID).Scan(&jobCount); err != nil {
			t.Fatalf("count first-node jobs: %v", err)
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

		waiting, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("wait for pending job: %v", err)
		}
		if waiting.Action != agents.PlatformUpdateRolloutStepWaitingHealth || waiting.ServerID != first.serverID || waiting.JobID != jobID {
			t.Fatalf("pending wait = %+v", waiting)
		}

		succeedJob(jobID)
		waiting, err = repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("wait for heartbeat: %v", err)
		}
		if waiting.Action != agents.PlatformUpdateRolloutStepWaitingHealth || waiting.WaitingReason != agents.PlatformUpdateRolloutHealthWaitingHeartbeat || waiting.ServerID != first.serverID || waiting.JobID != jobID {
			t.Fatalf("heartbeat wait = %+v", waiting)
		}

		heartbeat(first)
		healthy, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("prove first-node health: %v", err)
		}
		if healthy.Action != agents.PlatformUpdateRolloutStepNodeHealthy || healthy.RolloutStatus != agents.PlatformUpdateRolloutRunning || healthy.ServerID != first.serverID || healthy.JobID != jobID {
			t.Fatalf("healthy step = %+v", healthy)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, second.serverID).Scan(&jobCount); err != nil {
			t.Fatalf("count second-node jobs before later invocation: %v", err)
		}
		if jobCount != 0 {
			t.Fatalf("health-proof invocation admitted next node; second jobs=%d", jobCount)
		}

		next, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit next node on later invocation: %v", err)
		}
		if next.Action != agents.PlatformUpdateRolloutStepMutationAdmitted || next.ServerID != second.serverID || next.JobID == "" || next.JobID == jobID {
			t.Fatalf("next admission = %+v", next)
		}

		replay, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("replay second bound job: %v", err)
		}
		if replay.Action != agents.PlatformUpdateRolloutStepWaitingHealth || replay.ServerID != second.serverID || replay.JobID != next.JobID {
			t.Fatalf("second replay = %+v, want exact job %q", replay, next.JobID)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, second.serverID).Scan(&jobCount); err != nil {
			t.Fatalf("count second-node jobs after replay: %v", err)
		}
		if jobCount != 1 {
			t.Fatalf("second-node replay created %d jobs, want 1", jobCount)
		}
	})

	t.Run("all skipped completes without mutation and is terminal-idempotent", func(t *testing.T) {
		node := createReadyNode("RG-96E3f skipped")
		if _, err := pool.Exec(ctx, `UPDATE servers SET status = 'disabled' WHERE id = $1::uuid`, node.serverID); err != nil {
			t.Fatalf("disable skipped node: %v", err)
		}
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3", Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: node.serverID}},
		})
		if err != nil {
			t.Fatalf("persist skipped rollout: %v", err)
		}

		first, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("complete skipped rollout: %v", err)
		}
		second, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
		if err != nil {
			t.Fatalf("replay skipped terminal rollout: %v", err)
		}
		if first.Action != agents.PlatformUpdateRolloutStepSucceeded || second.Action != agents.PlatformUpdateRolloutStepSucceeded {
			t.Fatalf("skipped terminal results first=%+v second=%+v", first, second)
		}
		var jobCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, node.serverID).Scan(&jobCount); err != nil {
			t.Fatalf("count skipped jobs: %v", err)
		}
		if jobCount != 0 {
			t.Fatalf("all-skipped rollout created %d jobs", jobCount)
		}
	})

	t.Run("failed and outcome unknown stop durably", func(t *testing.T) {
		cases := []struct {
			name       string
			jobStatus  string
			wantAction agents.PlatformUpdateRolloutStepAction
		}{
			{name: "failed", jobStatus: "failed", wantAction: agents.PlatformUpdateRolloutStepFailed},
			{name: "outcome unknown", jobStatus: "outcome_unknown", wantAction: agents.PlatformUpdateRolloutStepOutcomeUnknown},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				node := createReadyNode("RG-96E3f terminal " + tc.jobStatus)
				rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
					TargetVersion: "v1.2.3", Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: node.serverID}},
				})
				if err != nil {
					t.Fatalf("persist terminal rollout: %v", err)
				}
				admitted, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
				if err != nil {
					t.Fatalf("admit terminal fixture: %v", err)
				}
				terminalizeJob(admitted.JobID, tc.jobStatus)

				stopped, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
				if err != nil {
					t.Fatalf("terminal step: %v", err)
				}
				if stopped.Action != tc.wantAction || stopped.ServerID != node.serverID || stopped.JobID != admitted.JobID {
					t.Fatalf("terminal result = %+v", stopped)
				}
				replay, err := repo.AdvancePlatformUpdateRollout(ctx, rolloutID)
				if err != nil {
					t.Fatalf("terminal replay: %v", err)
				}
				if replay.Action != tc.wantAction {
					t.Fatalf("terminal replay = %+v", replay)
				}
				var jobCount int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, node.serverID).Scan(&jobCount); err != nil {
					t.Fatalf("count terminal jobs: %v", err)
				}
				if jobCount != 1 {
					t.Fatalf("terminal replay created %d jobs, want 1", jobCount)
				}
			})
		}
	})
}
