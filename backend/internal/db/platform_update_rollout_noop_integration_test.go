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
)

func TestPlatformUpdateRolloutAllSkippedCompletesBeforeMutationOnlyManagerGate(t *testing.T) {
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
		VALUES ('RG-96E3e Manager-mismatch no-op', 'active', 'vpn')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create VPN server: %v", err)
	}
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "rg96e3e-noop-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: "rg96e3e-noop-token",
		Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
		Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{
			"schemaVersion": agents.PlatformUpdateCapabilitySchemaVersion,
			"state": agents.PlatformUpdateCapabilityStateReady,
			"request": agents.PlatformUpdateCapabilityRequestVersionOnly,
		}},
	}); err != nil {
		t.Fatalf("create ready Agent: %v", err)
	}

	// The target intentionally differs from the current Manager. Planning must
	// persist this node as skipped; admission must recognize that immutable no-op
	// membership before the Manager-version gate that protects actual mutations.
	rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, agents.PlatformUpdateRolloutPlan{
		TargetVersion: "v1.2.4",
		Entries:       []agents.PlatformUpdateRolloutPlanEntry{{ServerID: serverID}},
	})
	if err != nil {
		t.Fatalf("persist Manager-mismatch rollout: %v", err)
	}

	var plannedStatus string
	var blockers []string
	if err := pool.QueryRow(ctx, `
		SELECT status, planning_blockers
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid
	`, rolloutID).Scan(&plannedStatus, &blockers); err != nil {
		t.Fatalf("read Manager-mismatch planning snapshot: %v", err)
	}
	if plannedStatus != "skipped" || len(blockers) == 0 || blockers[0] != string(agents.PlatformUpdateRolloutBlockerManagerVersionMismatch) {
		t.Fatalf("planning snapshot status=%q blockers=%v", plannedStatus, blockers)
	}

	if _, err := repo.AdmitPlatformUpdateRolloutMutation(ctx, rolloutID); !errors.Is(err, agents.ErrPlatformUpdateRolloutComplete) {
		t.Fatalf("all-skipped Manager-mismatch admission error = %v, want ErrPlatformUpdateRolloutComplete", err)
	}

	var rolloutStatus, errorCode, entryStatus string
	var startedAt, completedAt *time.Time
	var jobCount int
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_code, ''), started_at, completed_at
		FROM platform_update_rollouts
		WHERE id = $1::uuid
	`, rolloutID).Scan(&rolloutStatus, &errorCode, &startedAt, &completedAt); err != nil {
		t.Fatalf("read completed no-op rollout: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT status
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid
	`, rolloutID).Scan(&entryStatus); err != nil {
		t.Fatalf("read completed no-op entry: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_platform_update_jobs
		WHERE server_id = $1::uuid
	`, serverID).Scan(&jobCount); err != nil {
		t.Fatalf("count no-op update jobs: %v", err)
	}

	if rolloutStatus != "succeeded" || errorCode != "" || entryStatus != "skipped" || startedAt == nil || completedAt == nil || jobCount != 0 {
		t.Fatalf("no-op result rollout=%q error=%q entry=%q started=%v completed=%v jobs=%d", rolloutStatus, errorCode, entryStatus, startedAt, completedAt, jobCount)
	}
}
