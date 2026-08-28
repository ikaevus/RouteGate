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

func TestPlatformUpdateJobHistoryIsImmutableAndRawAdmissionIsOrdered(t *testing.T) {
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

	repo := agents.NewRepository(pool)
	protocolVersion := buildinfo.AgentProtocolVersion
	now := time.Now().UTC()
	createReadyServer := func(name string) (string, string) {
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
		var agentID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE server_id = $1::uuid`, serverID).Scan(&agentID); err != nil {
			t.Fatalf("read Agent id: %v", err)
		}
		return serverID, agentID
	}

	serverA, agentA := createReadyServer("RG-96E3d immutable history A")
	serverB, agentB := createReadyServer("RG-96E3d immutable history B")

	job, err := repo.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{
		ServerID: serverA, TargetVersion: "v1.2.3",
	})
	if err != nil {
		t.Fatalf("create update job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE agent_platform_update_jobs
		SET status = 'in_progress', started_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, job.ID); err != nil {
		t.Fatalf("start update job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE agent_platform_update_jobs
		SET status = 'mutation_dispatched', dispatched_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, job.ID); err != nil {
		t.Fatalf("mark update job dispatched: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE agent_platform_update_jobs
		SET status = 'succeeded', completed_at = now(), updated_at = now()
		WHERE id = $1::uuid
	`, job.ID); err != nil {
		t.Fatalf("terminalize update job: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE agent_platform_update_jobs
		SET status = 'pending', started_at = NULL, dispatched_at = NULL, completed_at = NULL, updated_at = now()
		WHERE id = $1::uuid
	`, job.ID); err == nil {
		t.Fatal("terminal platform update job must not become dispatch-capable again")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM agent_platform_update_jobs WHERE id = $1::uuid`, job.ID); err == nil {
		t.Fatal("platform update job history deletion must be rejected")
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_platform_update_jobs SET server_id = $2::uuid WHERE id = $1::uuid`, job.ID, serverB); err == nil {
		t.Fatal("platform update job server identity mutation must be rejected")
	}

	var historyCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_platform_update_jobs WHERE id = $1::uuid AND server_id = $2::uuid`, job.ID, serverA).Scan(&historyCount); err != nil {
		t.Fatalf("read immutable update history: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("immutable update history count = %d, want 1", historyCount)
	}

	highServer, highAgent := serverA, agentA
	lowServer, lowAgent := serverB, agentB
	if highServer < lowServer {
		highServer, lowServer = lowServer, highServer
		highAgent, lowAgent = lowAgent, highAgent
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3'), ($3::uuid, $4::uuid, 'v1.2.3')
	`, highServer, highAgent, lowServer, lowAgent)
	if err == nil {
		t.Fatal("descending multi-row platform update admission must be rejected")
	}
	if !strings.Contains(err.Error(), "ascending canonical server_id order") {
		t.Fatalf("descending multi-row admission error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin raw multi-statement transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3')
	`, highServer, highAgent); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert first high-server job: %v", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3')
	`, lowServer, lowAgent)
	if err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("descending platform update admissions across one transaction must be rejected")
	}
	if !strings.Contains(err.Error(), "ascending canonical server_id order") {
		_ = tx.Rollback(ctx)
		t.Fatalf("descending transaction admission error = %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback raw multi-statement transaction: %v", err)
	}

	var pendingCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_platform_update_jobs
		WHERE server_id IN ($1::uuid, $2::uuid)
		  AND status = 'pending'
	`, serverA, serverB).Scan(&pendingCount); err != nil {
		t.Fatalf("count rolled-back raw jobs: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("descending raw admission left %d pending jobs", pendingCount)
	}
}
