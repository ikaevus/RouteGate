package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/observability"
)

func TestDiagnosticRunUsesAgentOperationTransportAndManagerEvaluation(t *testing.T) {
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
		VALUES ('Diagnostic fixture', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}

	protocolVersion := 1
	now := time.Now().UTC()
	tokenHash := "diagnostic-fixture-token-hash"
	_, err = agents.NewRepository(pool).CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "diagnostic-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "diagnostic-test",
		ProtocolVersion: &protocolVersion,
		TokenHash:       tokenHash,
		Capabilities: agents.Capabilities{
			"diagnosticProfiles": []string{observability.DiagnosticProfileHostOverview, observability.DiagnosticProfileVPNCoreStatus},
		},
		Status:       agents.StatusOnline,
		RegisteredAt: &now,
		LastSeenAt:   &now,
	})
	if err != nil {
		t.Fatalf("create diagnostic Agent: %v", err)
	}

	diagnostics := observability.NewDiagnosticRepository(pool)
	run, err := diagnostics.Create(ctx, serverID, observability.DiagnosticProfileHostOverview, "")
	if err != nil {
		t.Fatalf("create diagnostic run: %v", err)
	}
	if run.Status != "queued" || run.ProfileKey != observability.DiagnosticProfileHostOverview || run.AgentOperationJobID == "" {
		t.Fatalf("unexpected queued diagnostic run: %+v", run)
	}

	agentRepository := agents.NewRepository(pool)
	task, err := agentRepository.ClaimNextAgentOperationTask(ctx, tokenHash)
	if err != nil {
		t.Fatalf("claim diagnostic task: %v", err)
	}
	if task == nil || task.Kind != agents.AgentTaskKindDiagnostic || task.Operation != observability.DiagnosticProfileHostOverview {
		t.Fatalf("unexpected diagnostic task: %+v", task)
	}

	collectedAt := time.Now().UTC()
	resultPayload := map[string]any{
		"schemaVersion": 1,
		"profileKey":    observability.DiagnosticProfileHostOverview,
		"collectedAt":   collectedAt,
		"evidence": map[string]any{
			"available": true,
			"hostname":  "diagnostic-agent",
			"os":        "linux",
			"arch":      "amd64",
			"host": map[string]any{
				"memoryTotalBytes":     uint64(1000),
				"memoryAvailableBytes": uint64(800),
				"rootFsTotalBytes":     uint64(1000),
				"rootFsFreeBytes":      uint64(40),
				"uptimeSeconds":        uint64(3600),
			},
		},
	}
	kind, err := agentRepository.CompleteAgentOperationTask(ctx, agents.CompleteAgentOperationJobInput{
		TokenHash:     tokenHash,
		JobID:         task.ID,
		Status:        agents.AgentOperationJobStatusSucceeded,
		ResultPayload: resultPayload,
	})
	if err != nil {
		t.Fatalf("complete diagnostic task: %v", err)
	}
	if kind != agents.AgentTaskKindDiagnostic {
		t.Fatalf("completed kind=%q, want diagnostic", kind)
	}

	updated, err := diagnostics.SyncSemanticFromAgentJobs(ctx)
	if err != nil {
		t.Fatalf("project diagnostic result: %v", err)
	}
	if updated == 0 {
		t.Fatal("diagnostic projector did not update run")
	}

	completed, err := diagnostics.Get(ctx, serverID, run.ID)
	if err != nil {
		t.Fatalf("read completed diagnostic run: %v", err)
	}
	if completed.Status != "succeeded" || completed.State == nil || *completed.State != observability.HealthUnhealthy {
		t.Fatalf("unexpected completed diagnostic state: %+v", completed)
	}
	if completed.ReasonCode != "disk_free_critical" || completed.RecommendedAction != "free_disk_space" {
		t.Fatalf("Manager evaluation mismatch: reason=%q action=%q", completed.ReasonCode, completed.RecommendedAction)
	}
	if completed.ResultPayload["state"] != string(observability.HealthUnhealthy) {
		t.Fatalf("safe result payload missing Manager state: %#v", completed.ResultPayload)
	}
	if _, exists := completed.ResultPayload["command"]; exists {
		t.Fatalf("diagnostic result must not contain arbitrary command material: %#v", completed.ResultPayload)
	}
}

func TestDiagnosticRunRequiresAdvertisedProfile(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name, status) VALUES ('No diagnostics', 'active') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	protocolVersion := 1
	now := time.Now().UTC()
	_, err = agents.NewRepository(pool).CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "legacy-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "legacy", ProtocolVersion: &protocolVersion, TokenHash: "legacy-token",
		Capabilities: agents.Capabilities{"vpnCore": true}, Status: agents.StatusOnline,
		RegisteredAt: &now, LastSeenAt: &now,
	})
	if err != nil {
		t.Fatalf("create legacy Agent: %v", err)
	}

	if _, err := observability.NewDiagnosticRepository(pool).Create(ctx, serverID, observability.DiagnosticProfileHostOverview, ""); err == nil {
		t.Fatal("diagnostic run must not be created for Agent that did not advertise the profile")
	}
	var jobs, runs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_operation_jobs`).Scan(&jobs); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM observability_diagnostic_runs`).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if jobs != 0 || runs != 0 {
		t.Fatalf("atomic diagnostic creation violated: jobs=%d runs=%d", jobs, runs)
	}
}
