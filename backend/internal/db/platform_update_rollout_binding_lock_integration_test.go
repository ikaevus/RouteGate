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

func TestPlatformUpdateRolloutBindingRejectsJobFirstRawTransaction(t *testing.T) {
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

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status, deployment_role)
		VALUES ('RG-96E3d job-first binding fixture', 'active', 'vpn')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "rg96e3d-binding-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: "rg96e3d-binding-token",
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
		Entries: []agents.PlatformUpdateRolloutPlanEntry{{ServerID: serverID}},
	})
	if err != nil {
		t.Fatalf("persist rollout: %v", err)
	}

	var entryID, agentID string
	if err := pool.QueryRow(ctx, `
		SELECT e.id::text, a.id::text
		FROM platform_update_rollout_entries e
		JOIN agents a ON a.server_id = e.server_id
		WHERE e.rollout_id = $1::uuid
	`, rolloutID).Scan(&entryID, &agentID); err != nil {
		t.Fatalf("read rollout entry and Agent: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin raw job-first binding transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	var jobID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3')
		RETURNING id::text
	`, serverID, agentID).Scan(&jobID); err != nil {
		t.Fatalf("insert raw update job: %v", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET status = 'updating', platform_update_job_id = $2::uuid, updated_at = now()
		WHERE id = $1::uuid
	`, entryID, jobID)
	if err == nil {
		t.Fatal("job-first raw rollout binding update must be rejected")
	}
	if !strings.Contains(err.Error(), "parent must be established before binding update after server admission lock") {
		t.Fatalf("job-first raw rollout binding error = %v", err)
	}
}
