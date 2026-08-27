package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/jackc/pgx/v5"
)

func TestPlatformUpdateRolloutExecutionAdmissionIsAtomicAndReplaySafe(t *testing.T) {
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

	createReadyServer := func(name string) string {
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
		return serverID
	}

	serverID := createReadyServer("RG-96E3d replay fixture")
	rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
		TargetVersion: "v1.2.3",
		Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: serverID}},
	})
	if err != nil {
		t.Fatalf("persist rollout: %v", err)
	}

	first, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
	if err != nil {
		t.Fatalf("admit first rollout mutation: %v", err)
	}
	second, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID)
	if err != nil {
		t.Fatalf("replay rollout admission: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay created/rebound job %q, want %q", second.ID, first.ID)
	}

	var rolloutStatus, entryStatus, boundJobID string
	var jobCount int
	if err := pool.QueryRow(ctx, `SELECT status FROM platform_update_rollouts WHERE id = $1::uuid`, rolloutID).Scan(&rolloutStatus); err != nil {
		t.Fatalf("read rollout status: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT status, platform_update_job_id::text
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid
	`, rolloutID).Scan(&entryStatus, &boundJobID); err != nil {
		t.Fatalf("read rollout entry: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, serverID).Scan(&jobCount); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if rolloutStatus != "running" || entryStatus != "updating" || boundJobID != first.ID || jobCount != 1 {
		t.Fatalf("durable admission rollout=%q entry=%q bound=%q jobs=%d", rolloutStatus, entryStatus, boundJobID, jobCount)
	}

	staleServerID := createReadyServer("RG-96E3d stale fixture")
	staleRolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
		TargetVersion: "v1.2.3",
		Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: staleServerID}},
	})
	if err != nil {
		t.Fatalf("persist stale rollout: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE servers SET status = 'disabled' WHERE id = $1::uuid`, staleServerID); err != nil {
		t.Fatalf("disable stale server: %v", err)
	}
	if _, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, staleRolloutID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale admission error = %v, want pgx.ErrNoRows wrapping", err)
	}

	var staleStatus, staleErrorCode, staleEntryStatus string
	var staleBoundJobID *string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_code, '')
		FROM platform_update_rollouts
		WHERE id = $1::uuid
	`, staleRolloutID).Scan(&staleStatus, &staleErrorCode); err != nil {
		t.Fatalf("read stale rollout: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT status, platform_update_job_id::text
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid
	`, staleRolloutID).Scan(&staleEntryStatus, &staleBoundJobID); err != nil {
		t.Fatalf("read stale entry: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE server_id = $1::uuid`, staleServerID).Scan(&jobCount); err != nil {
		t.Fatalf("count stale jobs: %v", err)
	}
	if staleStatus != "failed" || staleErrorCode != "admission_rejected" || staleEntryStatus != "queued" || staleBoundJobID != nil || jobCount != 0 {
		t.Fatalf("stale admission rollout=%q error=%q entry=%q bound=%v jobs=%d", staleStatus, staleErrorCode, staleEntryStatus, staleBoundJobID, jobCount)
	}
}
