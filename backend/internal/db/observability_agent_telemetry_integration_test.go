package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestAgentTelemetryPersistenceKeepsOnlyLatestSnapshot(t *testing.T) {
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
		VALUES ('rg113b-telemetry-server', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create telemetry server: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agents (
			server_id, agent_version, version, status, token_hash, capabilities, registered_at, last_seen_at
		)
		VALUES ($1::uuid, 'dev', 'dev', 'online', 'rg113b-telemetry-hash', '{}'::jsonb, now(), now())
		RETURNING id::text
	`, serverID).Scan(&agentID); err != nil {
		t.Fatalf("create telemetry agent: %v", err)
	}

	logicalCPUs := 4
	memoryTotal := uint64(8 * 1024 * 1024 * 1024)
	memoryAvailable := uint64(4 * 1024 * 1024 * 1024)
	load1 := 0.25
	firstCollectedAt := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	store := agents.NewTelemetryStore(pool)
	first := agents.HeartbeatTelemetry{
		SchemaVersion: 1,
		CollectedAt:   firstCollectedAt,
		Host: agents.HeartbeatHostTelemetry{
			Load1:                &load1,
			LogicalCPUs:           &logicalCPUs,
			MemoryTotalBytes:      &memoryTotal,
			MemoryAvailableBytes:  &memoryAvailable,
		},
		VPNCore: agents.HeartbeatVPNCore{
			Type:         "sing-box",
			Installed:    true,
			Version:      "sing-box version 1.12.0",
			ServiceState: "active",
		},
	}
	if err := store.UpsertAgentTelemetry(ctx, agentID, serverID, first); err != nil {
		t.Fatalf("persist first telemetry snapshot: %v", err)
	}

	load1 = 0.75
	secondCollectedAt := firstCollectedAt.Add(time.Minute)
	second := first
	second.CollectedAt = secondCollectedAt
	second.Host.Load1 = &load1
	if err := store.UpsertAgentTelemetry(ctx, agentID, serverID, second); err != nil {
		t.Fatalf("persist second telemetry snapshot: %v", err)
	}

	var count int
	var persistedLoad float64
	var persistedCollectedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), MAX(host_load_1), MAX(collected_at)
		FROM observability_agent_telemetry
		WHERE agent_id = $1::uuid
	`, agentID).Scan(&count, &persistedLoad, &persistedCollectedAt); err != nil {
		t.Fatalf("read persisted telemetry: %v", err)
	}
	if count != 1 {
		t.Fatalf("telemetry row count = %d, want 1 latest snapshot", count)
	}
	if persistedLoad != 0.75 || !persistedCollectedAt.Equal(secondCollectedAt) {
		t.Fatalf("latest telemetry not preserved: load=%v collectedAt=%v", persistedLoad, persistedCollectedAt)
	}
}
