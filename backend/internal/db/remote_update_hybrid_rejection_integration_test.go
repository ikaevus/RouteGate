package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestRemotePlatformUpdateCreationRejectsHybridNode(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name, status, deployment_role) VALUES ('Hybrid remote rejection', 'active', 'hybrid') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatal(err)
	}
	protocolVersion := 1
	now := time.Now().UTC()
	_, err = agents.NewRepository(pool).CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "hybrid-agent", OS: "linux", Arch: "amd64", AgentVersion: "v1.0.0",
		ProtocolVersion: &protocolVersion, TokenHash: "hybrid-ready-token", Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
		Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{"schemaVersion": 1, "state": "ready", "request": "version_only"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agents.NewRepository(pool).CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{ServerID: serverID, TargetVersion: "v1.2.3"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("hybrid node was incorrectly enabled for VPN-only remote update: %v", err)
	}
}
