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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestRemotePlatformUpdateCreationRequiresExactReadyCapabilityAndOneActiveJob(t *testing.T) {
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
		INSERT INTO servers (name, status, deployment_role)
		VALUES ('Remote enablement fixture', 'active', 'vpn')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}

	protocolVersion := 1
	now := time.Now().UTC()
	tokenHash := "remote-enable-fixture-token"
	repository := agents.NewRepository(pool)
	agent, err := repository.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "remote-enable-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "v1.0.0",
		ProtocolVersion: &protocolVersion,
		TokenHash:       tokenHash,
		Capabilities: agents.Capabilities{
			"softwareUpdate": map[string]any{
				"schemaVersion": 1,
				"state":         "contract_only",
				"request":       "version_only",
			},
		},
		Status:       agents.StatusOnline,
		RegisteredAt: &now,
		LastSeenAt:   &now,
	})
	if err != nil {
		t.Fatalf("create Agent: %v", err)
	}

	if _, err := repository.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{ServerID: serverID, TargetVersion: "v1.2.3"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("contract-only Agent created update job: %v", err)
	}

	readyCapabilities := agents.Capabilities{
		"softwareUpdate": map[string]any{
			"schemaVersion": 1,
			"state":         "ready",
			"request":       "version_only",
		},
	}
	if _, err := pool.Exec(ctx, `UPDATE agents SET capabilities = $2::jsonb, updated_at = now() WHERE id = $1::uuid`, agent.ID, readyCapabilities); err != nil {
		t.Fatalf("mark Agent ready: %v", err)
	}

	job, err := repository.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{ServerID: serverID, TargetVersion: "v1.2.3"})
	if err != nil {
		t.Fatalf("create ready platform update: %v", err)
	}
	if job.ServerID != serverID || job.TargetVersion != "v1.2.3" || job.Status != agents.AgentOperationJobStatusPending || job.ID == "" {
		t.Fatalf("unexpected created job: %+v", job)
	}
	if job.StartedAt != nil || job.DispatchedAt != nil || job.CompletedAt != nil || job.ErrorCode != "" {
		t.Fatalf("new update job contains unexpected execution state: %+v", job)
	}

	if _, err := repository.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{ServerID: serverID, TargetVersion: "v1.2.4"}); err == nil {
		t.Fatal("second active update job was created")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("second active update returned unexpected error: %v", err)
		}
	}

	readBack, err := repository.GetPlatformUpdateJob(ctx, serverID, job.ID)
	if err != nil {
		t.Fatalf("read platform update job: %v", err)
	}
	if readBack.ID != job.ID || readBack.TargetVersion != job.TargetVersion || readBack.Status != job.Status {
		t.Fatalf("read-back mismatch: got=%+v want=%+v", readBack, job)
	}

	if _, err := repository.CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{ServerID: serverID, TargetVersion: " v1.2.5"}); err == nil {
		t.Fatal("whitespace-normalized target version was accepted")
	}
}

func TestRemotePlatformUpdateCreationRejectsManagementOnlyNode(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name, status, deployment_role) VALUES ('Manager only', 'active', 'management') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatal(err)
	}
	protocolVersion := 1
	now := time.Now().UTC()
	_, err = agents.NewRepository(pool).CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverID, Hostname: "management-agent", OS: "linux", Arch: "amd64", AgentVersion: "v1.0.0",
		ProtocolVersion: &protocolVersion, TokenHash: "management-ready-token", Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
		Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{"schemaVersion": 1, "state": "ready", "request": "version_only"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agents.NewRepository(pool).CreatePlatformUpdateJob(ctx, agents.CreatePlatformUpdateJobInput{ServerID: serverID, TargetVersion: "v1.2.3"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("management-only node was update-enabled: %v", err)
	}
}
