package db

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

func TestPlatformUpdateRolloutPlanningSnapshotPersistsAtomically(t *testing.T) {
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

	serverIDs := make([]string, 3)
	for i := range serverIDs {
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role)
			VALUES ($1, 'active', 'vpn')
			RETURNING id::text
		`, fmt.Sprintf("RG-96E3c snapshot fixture %d", i+1)).Scan(&serverIDs[i]); err != nil {
			t.Fatalf("create server fixture %d: %v", i, err)
		}
	}

	previousVersion := buildinfo.Version
	buildinfo.Version = "v1.2.3"
	t.Cleanup(func() { buildinfo.Version = previousVersion })
	protocolVersion := buildinfo.AgentProtocolVersion
	now := time.Now().UTC()
	repo := agents.NewRepository(pool)
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverIDs[0], Hostname: "rollout-ready-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: "rollout-ready-token",
		Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
		Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{
			"schemaVersion": agents.PlatformUpdateCapabilitySchemaVersion,
			"state": agents.PlatformUpdateCapabilityStateReady,
			"request": agents.PlatformUpdateCapabilityRequestVersionOnly,
		}},
	}); err != nil {
		t.Fatalf("create ready Agent: %v", err)
	}
	if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{
		ServerID: serverIDs[2], Hostname: "rollout-additive-capability-agent", OS: "linux", Arch: "amd64",
		AgentVersion: "v1.2.3", ProtocolVersion: &protocolVersion, TokenHash: "rollout-additive-token",
		Status: agents.StatusOnline, RegisteredAt: &now, LastSeenAt: &now,
		Capabilities: agents.Capabilities{"softwareUpdate": map[string]any{
			"schemaVersion": agents.PlatformUpdateCapabilitySchemaVersion,
			"state": agents.PlatformUpdateCapabilityStateReady,
			"request": agents.PlatformUpdateCapabilityRequestVersionOnly,
			"unexpected": true,
		}},
	}); err != nil {
		t.Fatalf("create additive-capability Agent: %v", err)
	}

	// Deliberately fabricate caller-supplied eligibility values. Persistence
	// must ignore them and re-derive the authoritative snapshot from DB state.
	plan := agents.PlatformUpdateRolloutPlan{
		TargetVersion: "v1.2.3",
		Entries: []agents.PlatformUpdateRolloutPlanEntry{
			{ServerID: serverIDs[0], Eligible: false, Blockers: []agents.PlatformUpdateRolloutBlocker{agents.PlatformUpdateRolloutBlockerServerDisabled}},
			{ServerID: serverIDs[1], Eligible: true},
			{ServerID: serverIDs[2], Eligible: true},
		},
	}

	rolloutID, err := repo.PersistPlatformUpdateRolloutPlan(ctx, plan)
	if err != nil {
		t.Fatalf("persist rollout planning snapshot: %v", err)
	}

	var rolloutStatus, targetVersion string
	if err := pool.QueryRow(ctx, `
		SELECT status, target_version
		FROM platform_update_rollouts
		WHERE id = $1::uuid
	`, rolloutID).Scan(&rolloutStatus, &targetVersion); err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	if rolloutStatus != "pending" || targetVersion != plan.TargetVersion {
		t.Fatalf("rollout status=%q target=%q", rolloutStatus, targetVersion)
	}

	rows, err := pool.Query(ctx, `
		SELECT server_id::text, position, status, planning_blockers, completed_at IS NOT NULL
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid
		ORDER BY position
	`, rolloutID)
	if err != nil {
		t.Fatalf("read rollout entries: %v", err)
	}
	defer rows.Close()

	type persistedEntry struct {
		serverID    string
		position    int
		status      string
		blockers    []string
		isCompleted bool
	}
	var got []persistedEntry
	for rows.Next() {
		var entry persistedEntry
		if err := rows.Scan(&entry.serverID, &entry.position, &entry.status, &entry.blockers, &entry.isCompleted); err != nil {
			t.Fatalf("scan rollout entry: %v", err)
		}
		got = append(got, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rollout entries: %v", err)
	}

	want := []persistedEntry{
		{serverID: serverIDs[0], position: 0, status: "queued", blockers: []string{}},
		{serverID: serverIDs[1], position: 1, status: "skipped", blockers: []string{"agent_missing", "update_capability_not_ready", "agent_protocol_incompatible"}, isCompleted: true},
		{serverID: serverIDs[2], position: 2, status: "skipped", blockers: []string{"update_capability_not_ready"}, isCompleted: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted rollout entries = %#v, want %#v", got, want)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET planning_blockers = ARRAY['server_disabled']::text[]
		WHERE rollout_id = $1::uuid AND position = 1
	`, rolloutID); err == nil {
		t.Fatal("planning blockers remained mutable after snapshot persistence")
	}
}
