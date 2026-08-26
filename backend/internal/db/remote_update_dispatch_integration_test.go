package db

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestRemotePlatformUpdateDispatchIsAtMostOnceAndReconciliationOnlyAfterClaim(t *testing.T) {
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
		VALUES ('Remote update fixture', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}

	protocolVersion := 1
	now := time.Now().UTC()
	tokenHash := "remote-update-fixture-token"
	agent, err := agents.NewRepository(pool).CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "remote-update-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "remote-update-test",
		ProtocolVersion: &protocolVersion,
		TokenHash:       tokenHash,
		Capabilities: agents.Capabilities{
			"softwareUpdate": map[string]any{
				"schemaVersion": 1,
				"state":         "contract_only",
				"request":       "version_only",
			},
		},
		Status:       agents.StatusOnline,
		RegisteredAt: &now,
		LastSeenAt:   &now,
	})
	if err != nil {
		t.Fatalf("create Agent: %v", err)
	}

	repository := agents.NewRepository(pool)
	insertJob := func(version string) string {
		t.Helper()
		var jobID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
			VALUES ($1::uuid, $2::uuid, $3)
			RETURNING id::text
		`, serverID, agent.ID, version).Scan(&jobID); err != nil {
			t.Fatalf("insert platform update job: %v", err)
		}
		return jobID
	}
	ageJob := func(jobID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET updated_at = now() - interval '2 seconds'
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("age platform update job: %v", err)
		}
	}
	assertStatus := func(jobID, want string, wantDispatched bool) {
		t.Helper()
		var status string
		var dispatched bool
		if err := pool.QueryRow(ctx, `
			SELECT status, dispatched_at IS NOT NULL
			FROM agent_platform_update_jobs
			WHERE id = $1::uuid
		`, jobID).Scan(&status, &dispatched); err != nil {
			t.Fatalf("read platform update job: %v", err)
		}
		if status != want || dispatched != wantDispatched {
			t.Fatalf("job %s state=(%s, dispatched=%v), want=(%s, dispatched=%v)", jobID, status, dispatched, want, wantDispatched)
		}
	}

	// First claim is the one and only dispatch-capable claim.
	jobID := insertJob("v1.2.3")
	task, err := repository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil {
		t.Fatalf("claim dispatch task: %v", err)
	}
	if task == nil || task.ID != jobID || task.Kind != agents.AgentTaskKindPlatformUpdate || task.Operation != agents.PlatformUpdateOperationDispatch || task.Status != agents.AgentOperationJobStatusInProgress {
		t.Fatalf("unexpected dispatch task: %+v", task)
	}
	var request map[string]any
	if err := json.Unmarshal(task.RenderedConfig, &request); err != nil {
		t.Fatalf("decode dispatch payload: %v", err)
	}
	if len(request) != 2 || request["targetVersion"] != "v1.2.3" || request["schemaVersion"] != float64(1) {
		t.Fatalf("unexpected dispatch payload: %#v", request)
	}

	// If the dispatch acknowledgement is lost, the same in_progress row is
	// returned only as reconcile; it is never redispatched.
	ageJob(jobID)
	reconcile, err := repository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil {
		t.Fatalf("claim reconciliation after lost ack: %v", err)
	}
	if reconcile == nil || reconcile.ID != jobID || reconcile.Operation != agents.PlatformUpdateOperationReconcile {
		t.Fatalf("in_progress job was not reconciliation-only: %+v", reconcile)
	}
	kind, err := repository.CompleteAgentOperationTask(ctx, agents.CompleteAgentOperationJobInput{
		TokenHash: tokenHash,
		JobID:     jobID,
		Status:    agents.AgentOperationJobStatusSucceeded,
		ResultPayload: map[string]any{
			"taskId":        jobID,
			"targetVersion": "v1.2.3",
			"status":        agents.PlatformUpdateReceiptStatusPending,
		},
	})
	if err != nil || kind != agents.AgentTaskKindPlatformUpdate {
		t.Fatalf("reconcile prepared receipt: kind=%q err=%v", kind, err)
	}
	assertStatus(jobID, agents.AgentOperationJobStatusMutationDispatched, true)

	// Post-dispatch receipt failure terminalizes only through reconciliation and
	// preserves dispatched provenance.
	ageJob(jobID)
	reconcile, err = repository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil || reconcile == nil || reconcile.Operation != agents.PlatformUpdateOperationReconcile {
		t.Fatalf("claim post-dispatch reconciliation: task=%+v err=%v", reconcile, err)
	}
	kind, err = repository.CompleteAgentOperationTask(ctx, agents.CompleteAgentOperationJobInput{
		TokenHash: tokenHash,
		JobID:     jobID,
		Status:    agents.AgentOperationJobStatusSucceeded,
		ResultPayload: map[string]any{
			"taskId":        jobID,
			"targetVersion": "v1.2.3",
			"status":        agents.PlatformUpdateReceiptStatusFailed,
			"code":          "verified_updater_failed",
		},
	})
	if err != nil || kind != agents.AgentTaskKindPlatformUpdate {
		t.Fatalf("terminalize reconciled failure: kind=%q err=%v", kind, err)
	}
	assertStatus(jobID, agents.AgentOperationJobStatusFailed, true)

	// A second job may run only after the first is terminal. Direct successful
	// systemd acceptance is mutation_dispatched, never update success.
	jobID = insertJob("v1.2.4")
	task, err = repository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil || task == nil || task.Operation != agents.PlatformUpdateOperationDispatch {
		t.Fatalf("claim second dispatch: task=%+v err=%v", task, err)
	}
	kind, err = repository.CompleteAgentOperationTask(ctx, agents.CompleteAgentOperationJobInput{
		TokenHash: tokenHash,
		JobID:     jobID,
		Status:    agents.AgentOperationJobStatusSucceeded,
		ResultPayload: map[string]any{
			"taskId":        jobID,
			"targetVersion": "v1.2.4",
			"status":        agents.PlatformUpdateReceiptStatusMutationDispatched,
		},
	})
	if err != nil || kind != agents.AgentTaskKindPlatformUpdate {
		t.Fatalf("acknowledge detached dispatch: kind=%q err=%v", kind, err)
	}
	assertStatus(jobID, agents.AgentOperationJobStatusMutationDispatched, true)

	// Finish it so the deterministic pre-dispatch failure case can create a new
	// active job without violating the partial unique index.
	ageJob(jobID)
	_, err = repository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil {
		t.Fatalf("claim reconcile before success: %v", err)
	}
	_, err = repository.CompleteAgentOperationTask(ctx, agents.CompleteAgentOperationJobInput{
		TokenHash: tokenHash,
		JobID:     jobID,
		Status:    agents.AgentOperationJobStatusSucceeded,
		ResultPayload: map[string]any{
			"taskId":        jobID,
			"targetVersion": "v1.2.4",
			"status":        agents.PlatformUpdateReceiptStatusSucceeded,
		},
	})
	if err != nil {
		t.Fatalf("terminalize success: %v", err)
	}
	assertStatus(jobID, agents.AgentOperationJobStatusSucceeded, true)

	// Deterministic failure is allowed only before dispatch and must not carry raw
	// error text or a dispatched timestamp.
	jobID = insertJob("v1.2.5")
	task, err = repository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil || task == nil || task.Operation != agents.PlatformUpdateOperationDispatch {
		t.Fatalf("claim deterministic-failure dispatch: task=%+v err=%v", task, err)
	}
	kind, err = repository.CompleteAgentOperationTask(ctx, agents.CompleteAgentOperationJobInput{
		TokenHash: tokenHash,
		JobID:     jobID,
		Status:    agents.AgentOperationJobStatusFailed,
		ResultPayload: map[string]any{
			"taskId":        jobID,
			"targetVersion": "v1.2.5",
			"status":        agents.PlatformUpdateReceiptStatusFailed,
			"code":          "pre_dispatch_failed",
		},
	})
	if err != nil || kind != agents.AgentTaskKindPlatformUpdate {
		t.Fatalf("complete deterministic pre-dispatch failure: kind=%q err=%v", kind, err)
	}
	assertStatus(jobID, agents.AgentOperationJobStatusFailed, false)
}
