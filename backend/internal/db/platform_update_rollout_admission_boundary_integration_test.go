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

func TestPlatformUpdateAdmissionBoundaryGuardsRawInserts(t *testing.T) {
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

	// The global admission mutex must be acquired at statement level so an
	// INSERT ... SELECT source cannot take row locks before the mutex.
	var statementTriggerCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		WHERE NOT t.tgisinternal
		  AND t.tgname IN (
		      'trg_agent_platform_update_jobs_insert_admission_lock',
		      'trg_platform_update_rollout_entries_insert_admission_lock'
		  )
		  AND c.relname IN ('agent_platform_update_jobs', 'platform_update_rollout_entries')
		  AND (t.tgtype & 2) = 2
		  AND (t.tgtype & 4) = 4
		  AND (t.tgtype & 1) = 0
	`).Scan(&statementTriggerCount); err != nil {
		t.Fatalf("inspect admission statement triggers: %v", err)
	}
	if statementTriggerCount != 2 {
		t.Fatalf("statement-level admission INSERT triggers = %d, want 2", statementTriggerCount)
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
		VALUES ('RG-96E3d active-job snapshot fixture', 'active', 'vpn')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "rg96e3d-active-snapshot-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: "rg96e3d-active-snapshot-token",
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
		t.Fatalf("read ready Agent id: %v", err)
	}

	if _, err := repo.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{
		ServerID: serverID,
		TargetVersion: "v1.2.3",
	}); err != nil {
		t.Fatalf("create active direct update job: %v", err)
	}

	var rolloutID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO platform_update_rollouts (target_version)
		VALUES ('v1.2.3')
		RETURNING id::text
	`).Scan(&rolloutID); err != nil {
		t.Fatalf("create raw pending rollout: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO platform_update_rollout_entries (
			rollout_id, server_id, target_version, position, status, planning_blockers
		)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3', 0, 'queued', ARRAY[]::text[])
	`, rolloutID, serverID)
	if err == nil {
		t.Fatal("queued raw rollout snapshot must reject a server with an active update job")
	}
	if !strings.Contains(err.Error(), "active or unresolved update job") {
		t.Fatalf("active-job snapshot rejection error = %v", err)
	}

	var entryCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM platform_update_rollout_entries WHERE rollout_id = $1::uuid
	`, rolloutID).Scan(&entryCount); err != nil {
		t.Fatalf("count rejected rollout entries: %v", err)
	}
	if entryCount != 0 {
		t.Fatalf("rejected active-job snapshot left %d rollout entries, want 0", entryCount)
	}

	// A raw transaction can retain a rollout row lock before it reaches an
	// admission statement. If another trusted admission already owns the global
	// mutex, the raw statement must fail immediately rather than wait for the
	// mutex and form parent<->mutex deadlock with that trusted transaction.
	prelockedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin prelocked raw transaction: %v", err)
	}
	defer prelockedTx.Rollback(ctx)
	if _, err := prelockedTx.Exec(ctx, `
		SELECT 1 FROM platform_update_rollouts WHERE id = $1::uuid FOR UPDATE
	`, rolloutID); err != nil {
		t.Fatalf("prelock rollout row: %v", err)
	}

	mutexTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mutex owner transaction: %v", err)
	}
	defer mutexTx.Rollback(ctx)
	if _, err := mutexTx.Exec(ctx, `SELECT lock_platform_update_admission_global()`); err != nil {
		t.Fatalf("acquire trusted admission mutex: %v", err)
	}

	attemptCtx, attemptCancel := context.WithTimeout(ctx, 2*time.Second)
	defer attemptCancel()
	_, err = prelockedTx.Exec(attemptCtx, `
		INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
		VALUES ($1::uuid, $2::uuid, 'v1.2.3')
	`, serverID, agentID)
	if err == nil {
		t.Fatal("prelocked raw admission must fail while trusted mutex is owned")
	}
	if !strings.Contains(err.Error(), "admission mutex is busy") {
		t.Fatalf("prelocked raw admission error = %v", err)
	}
	if attemptCtx.Err() != nil {
		t.Fatalf("prelocked raw admission waited instead of failing fast: %v", attemptCtx.Err())
	}
}
