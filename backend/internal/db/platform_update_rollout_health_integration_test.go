package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

func TestPlatformUpdateRolloutHealthProofGatesAdvancement(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
			t.Fatalf("start update job: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'mutation_dispatched', dispatched_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("dispatch update job: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET status = 'succeeded', completed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("succeed update job: %v", err)
		}
	}

	heartbeat := func(token string) {
		t.Helper()
		version := "v1.2.3"
		if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
			TokenHash: token, AgentVersion: &version, ProtocolVersion: &protocolVersion,
		}); err != nil {
			t.Fatalf("record authenticated heartbeat: %v", err)
		}
	}

	t.Run("fresh exact proof unlocks only the next persisted node", func(t *testing.T) {
		first := createReadyNode("RG-96E3e first")
		second := createReadyNode("RG-96E3e second")
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3",
			Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: first.serverID}, {ServerID: second.serverID}},
		})
		if err != nil {
			t.Fatalf("persist rollout: %v", err)
		}
		job, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit first mutation: %v", err)
		}
		succeedJob(job.ID)

		// The database boundary must reject a caller that tries to manufacture
		// healthy before the bearer-authenticated post-completion heartbeat exists.
		if _, err := pool.Exec(ctx, `
			UPDATE platform_update_rollout_entries
			SET status = 'healthy', completed_at = clock_timestamp(), updated_at = clock_timestamp()
			WHERE rollout_id = $1::uuid AND status = 'updating'
		`, rolloutID); err == nil || !strings.Contains(err.Error(), "fresh post-completion authenticated heartbeat") {
			t.Fatalf("raw healthy transition before proof error = %v", err)
		}

		heartbeat(first.token)
		result, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile fresh proof: %v", err)
		}
		if result.RolloutStatus != "running" || result.EntryStatus != "healthy" {
			t.Fatalf("fresh proof result = %+v", result)
		}

		next, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit second mutation after proof: %v", err)
		}
		if next.ServerID != second.serverID || next.ID == job.ID {
			t.Fatalf("next mutation = %+v, want second server %s", next, second.serverID)
		}
		var firstStatus, secondStatus string
		if err := pool.QueryRow(ctx, `
			SELECT
				max(status) FILTER (WHERE server_id = $2::uuid),
				max(status) FILTER (WHERE server_id = $3::uuid)
			FROM platform_update_rollout_entries
			WHERE rollout_id = $1::uuid
		`, rolloutID, first.serverID, second.serverID).Scan(&firstStatus, &secondStatus); err != nil {
			t.Fatalf("read rollout entry statuses: %v", err)
		}
		if firstStatus != "healthy" || secondStatus != "updating" {
			t.Fatalf("entry statuses first=%q second=%q", firstStatus, secondStatus)
		}
	})

	t.Run("stale heartbeat remains fail-closed waiting", func(t *testing.T) {
		node := createReadyNode("RG-96E3e stale heartbeat")
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3", Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: node.serverID}},
		})
		if err != nil {
			t.Fatalf("persist stale rollout: %v", err)
		}
		job, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit stale fixture: %v", err)
		}
		succeedJob(job.ID)
		heartbeat(node.token)
		if _, err := pool.Exec(ctx, `
			UPDATE agents
			SET last_authenticated_heartbeat_at = clock_timestamp() - interval '3 minutes'
			WHERE server_id = $1::uuid
		`, node.serverID); err != nil {
			t.Fatalf("age heartbeat proof: %v", err)
		}
		result, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile stale proof: %v", err)
		}
		if result.WaitingReason != agents.PlatformUpdateRolloutHealthWaitingHeartbeat || result.EntryStatus != "updating" {
			t.Fatalf("stale proof result = %+v", result)
		}
	})

	t.Run("credential replacement invalidates old bearer proof", func(t *testing.T) {
		node := createReadyNode("RG-96E3e generation")
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3", Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: node.serverID}},
		})
		if err != nil {
			t.Fatalf("persist generation rollout: %v", err)
		}
		job, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit generation fixture: %v", err)
		}
		succeedJob(job.ID)
		heartbeat(node.token)

		newToken := node.token + "-replacement"
		if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
			ServerID: node.serverID, Hostname: "replacement-agent", OS: "linux", Arch: "amd64",
			AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: newToken,
			Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
			Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{
				"schemaVersion": agents.PlatformUpdateCapabilitySchemaVersion,
				"state": agents.PlatformUpdateCapabilityStateReady,
				"request": agents.PlatformUpdateCapabilityRequestVersionOnly,
			}},
		}); err != nil {
			t.Fatalf("replace Agent credentials: %v", err)
		}

		waiting, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile after replacement: %v", err)
		}
		if waiting.WaitingReason != agents.PlatformUpdateRolloutHealthWaitingHeartbeat {
			t.Fatalf("replacement reused old proof: %+v", waiting)
		}

		heartbeat(newToken)
		healthy, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile replacement heartbeat: %v", err)
		}
		if healthy.RolloutStatus != "succeeded" || healthy.EntryStatus != "healthy" {
			t.Fatalf("replacement fresh proof result = %+v", healthy)
		}
	})

	t.Run("intervening job history permanently stops rollout", func(t *testing.T) {
		node := createReadyNode("RG-96E3e intervening")
		rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
			TargetVersion: "v1.2.3", Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: node.serverID}},
		})
		if err != nil {
			t.Fatalf("persist intervening rollout: %v", err)
		}
		job, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
		if err != nil {
			t.Fatalf("admit intervening fixture: %v", err)
		}
		succeedJob(job.ID)
		if _, err := repo.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{
			ServerID: node.serverID, TargetVersion: "v1.2.3",
		}); err != nil {
			t.Fatalf("create intervening direct job: %v", err)
		}
		result, err := repo.ReconcilePlatformUpdateRolloutHealth(ctx, rolloutID)
		if err != nil {
			t.Fatalf("reconcile intervening history: %v", err)
		}
		if result.RolloutStatus != "failed" || result.EntryStatus != "failed" {
			t.Fatalf("intervening history result = %+v", result)
		}
		var errorCode string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(error_code, '') FROM platform_update_rollouts WHERE id = $1::uuid`, rolloutID).Scan(&errorCode); err != nil {
			t.Fatalf("read intervening failure: %v", err)
		}
		if errorCode != agents.PlatformUpdateRolloutHealthInterveningHistory {
			t.Fatalf("intervening error code = %q", errorCode)
		}
	})
}
