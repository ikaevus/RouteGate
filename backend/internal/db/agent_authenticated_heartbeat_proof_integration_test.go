package db

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

func TestAgentCredentialGenerationAndAuthenticatedHeartbeatProof(t *testing.T) {
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
		VALUES ('Authenticated heartbeat proof fixture', 'pending')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server fixture: %v", err)
	}

	repo := agents.NewRepository(pool)
	protocolVersion := 1
	registeredAt := time.Now().UTC()

	first, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "proof-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "proof-test-v1",
		ProtocolVersion: &protocolVersion,
		TokenHash:       "proof-old-token",
		Status:          agents.StatusRegistered,
		RegisteredAt:    &registeredAt,
	})
	if err != nil {
		t.Fatalf("register first agent credential: %v", err)
	}
	assertAgentHeartbeatProof(t, ctx, pool, serverID, 1, false)

	if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
		TokenHash: "proof-old-token",
	}); err != nil {
		t.Fatalf("write bearer-authenticated heartbeat proof: %v", err)
	}
	assertAgentHeartbeatProof(t, ctx, pool, serverID, 1, true)

	second, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "proof-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "proof-test-v2",
		ProtocolVersion: &protocolVersion,
		TokenHash:       "proof-new-token",
		Status:          agents.StatusRegistered,
		RegisteredAt:    &registeredAt,
	})
	if err != nil {
		t.Fatalf("replace agent credential: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replacement agent id = %q, want existing id %q", second.ID, first.ID)
	}
	assertAgentHeartbeatProof(t, ctx, pool, serverID, 2, false)

	if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
		TokenHash: "proof-old-token",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale bearer heartbeat error = %v, want pgx.ErrNoRows", err)
	}
	assertAgentHeartbeatProof(t, ctx, pool, serverID, 2, false)

	if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
		AgentID: second.ID,
	}); err != nil {
		t.Fatalf("write internal AgentID heartbeat: %v", err)
	}
	assertAgentHeartbeatProof(t, ctx, pool, serverID, 2, false)

	if _, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{
		TokenHash: "proof-new-token",
	}); err != nil {
		t.Fatalf("write replacement bearer heartbeat proof: %v", err)
	}
	assertAgentHeartbeatProof(t, ctx, pool, serverID, 2, true)
}

func TestAuthenticatedHeartbeatUsesPostLockWallClockForLiveness(t *testing.T) {
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
		VALUES ('Authenticated heartbeat post-lock fixture', 'pending')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server fixture: %v", err)
	}

	repo := agents.NewRepository(pool)
	protocolVersion := 1
	registeredAt := time.Now().UTC()
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID:        serverID,
		Hostname:        "post-lock-proof-agent",
		OS:              "linux",
		Arch:            "amd64",
		AgentVersion:    "proof-test-v1",
		ProtocolVersion: &protocolVersion,
		TokenHash:       "post-lock-proof-token",
		Status:          agents.StatusRegistered,
		RegisteredAt:    &registeredAt,
	}); err != nil {
		t.Fatalf("register Agent fixture: %v", err)
	}

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire Agent lock connection: %v", err)
	}
	defer lockConn.Release()
	lockTx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Agent lock transaction: %v", err)
	}
	defer lockTx.Rollback(ctx)
	if _, err := lockTx.Exec(ctx, `
		SELECT 1
		FROM agents
		WHERE server_id = $1::uuid
		FOR UPDATE
	`, serverID); err != nil {
		t.Fatalf("lock Agent row: %v", err)
	}

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{TokenHash: "post-lock-proof-token"})
		done <- err
	}()
	<-started
	// Give the heartbeat transaction enough time to begin and wait on the row.
	time.Sleep(50 * time.Millisecond)

	var releaseMarker time.Time
	if err := lockTx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&releaseMarker); err != nil {
		t.Fatalf("capture lock release marker: %v", err)
	}
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release Agent row lock: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write blocked bearer heartbeat: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked bearer heartbeat did not complete after Agent row unlock")
	}

	var lastSeenAt, heartbeatAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT last_seen_at, last_authenticated_heartbeat_at
		FROM agents
		WHERE server_id = $1::uuid
	`, serverID).Scan(&lastSeenAt, &heartbeatAt); err != nil {
		t.Fatalf("read post-lock heartbeat timestamps: %v", err)
	}
	if !lastSeenAt.Equal(heartbeatAt) {
		t.Fatalf("liveness timestamp %s differs from authenticated proof %s", lastSeenAt, heartbeatAt)
	}
	if !lastSeenAt.After(releaseMarker) {
		t.Fatalf("heartbeat timestamp %s is not after row-lock release marker %s", lastSeenAt, releaseMarker)
	}
}

func assertAgentHeartbeatProof(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	serverID string,
	wantGeneration int64,
	wantProof bool,
) {
	t.Helper()

	var (
		generation   int64
		heartbeatAt  sql.NullTime
		heartbeatGen sql.NullInt64
	)
	if err := pool.QueryRow(ctx, `
		SELECT
			credential_generation,
			last_authenticated_heartbeat_at,
			last_authenticated_heartbeat_generation
		FROM agents
		WHERE server_id = $1::uuid
	`, serverID).Scan(&generation, &heartbeatAt, &heartbeatGen); err != nil {
		t.Fatalf("read authenticated heartbeat proof: %v", err)
	}

	if generation != wantGeneration {
		t.Fatalf("credential_generation = %d, want %d", generation, wantGeneration)
	}
	if heartbeatAt.Valid != heartbeatGen.Valid {
		t.Fatalf(
			"heartbeat proof pair mismatch: timestamp valid=%v generation valid=%v",
			heartbeatAt.Valid,
			heartbeatGen.Valid,
		)
	}
	if heartbeatAt.Valid != wantProof {
		t.Fatalf("authenticated heartbeat proof present = %v, want %v", heartbeatAt.Valid, wantProof)
	}
	if heartbeatGen.Valid && heartbeatGen.Int64 != generation {
		t.Fatalf(
			"heartbeat proof generation = %d, want current credential generation %d",
			heartbeatGen.Int64,
			generation,
		)
	}
}
