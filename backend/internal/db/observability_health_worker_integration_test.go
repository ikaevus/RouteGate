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

func TestHealthWorkerEvaluatesFreshAndStaleTelemetry(t *testing.T) {
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

	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status)
		VALUES ('rg113c-health-server', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create health server: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agents (
			server_id, agent_version, version, status, token_hash, capabilities, registered_at, last_seen_at
		)
		VALUES ($1::uuid, 'dev', 'dev', 'online', 'rg113c-health-hash', '{}'::jsonb, now(), now())
		RETURNING id::text
	`, serverID).Scan(&agentID); err != nil {
		t.Fatalf("create health agent: %v", err)
	}

	logicalCPUs := 4
	memoryTotal := uint64(8 * 1024 * 1024 * 1024)
	memoryAvailable := uint64(4 * 1024 * 1024 * 1024)
	rootTotal := uint64(100 * 1024 * 1024 * 1024)
	rootFree := uint64(40 * 1024 * 1024 * 1024)
	collectedAt := time.Now().UTC()
	telemetry := agents.HeartbeatTelemetry{
		SchemaVersion: 1,
		CollectedAt:   collectedAt,
		Host: agents.HeartbeatHostTelemetry{
			LogicalCPUs:          &logicalCPUs,
			MemoryTotalBytes:     &memoryTotal,
			MemoryAvailableBytes: &memoryAvailable,
			RootFSTotalBytes:     &rootTotal,
			RootFSFreeBytes:      &rootFree,
		},
		VPNCore: agents.HeartbeatVPNCore{
			Type:         "sing-box",
			Installed:    true,
			Version:      "sing-box version 1.12.0",
			ServiceState: "active",
		},
	}
	if err := agents.NewTelemetryStore(pool).UpsertAgentTelemetry(ctx, agentID, serverID, telemetry); err != nil {
		t.Fatalf("persist telemetry snapshot: %v", err)
	}

	var receivedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT received_at
		FROM observability_agent_telemetry
		WHERE agent_id=$1::uuid
	`, agentID).Scan(&receivedAt); err != nil {
		t.Fatalf("read telemetry received_at: %v", err)
	}

	worker := observability.NewHealthWorker(logger, pool)
	if err := worker.EvaluateOnce(ctx, receivedAt.Add(time.Second)); err != nil {
		t.Fatalf("evaluate fresh telemetry: %v", err)
	}

	var currentCount int
	var nonHealthyCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE state <> 'healthy')
		FROM observability_current_health
		WHERE resource_type='server' AND resource_id=$1
	`, serverID).Scan(&currentCount, &nonHealthyCount); err != nil {
		t.Fatalf("read fresh health checks: %v", err)
	}
	if currentCount != 4 || nonHealthyCount != 0 {
		t.Fatalf("fresh health checks: count=%d nonHealthy=%d, want 4/0", currentCount, nonHealthyCount)
	}

	if err := worker.EvaluateOnce(ctx, receivedAt.Add(observability.AgentTelemetryHealthTTL)); err != nil {
		t.Fatalf("evaluate stale telemetry: %v", err)
	}

	var unknownCount int
	var transitionCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE state='unknown')
		FROM observability_current_health
		WHERE resource_type='server' AND resource_id=$1
	`, serverID).Scan(&unknownCount); err != nil {
		t.Fatalf("read stale health checks: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM observability_health_transitions
		WHERE resource_type='server' AND resource_id=$1
	`, serverID).Scan(&transitionCount); err != nil {
		t.Fatalf("count health transitions: %v", err)
	}
	if unknownCount != 4 {
		t.Fatalf("stale unknown checks = %d, want 4", unknownCount)
	}
	if transitionCount != 8 {
		t.Fatalf("health transitions = %d, want 8 (initial + stale)", transitionCount)
	}
}
