package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func TestAutomaticSelectionPreviewAndApplyUseFreshCandidateEvidence(t *testing.T) {
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

	serverIDs := make([]string, 2)
	for index, name := range []string{"selection-current", "selection-preferred"} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role, vpn_protocol)
			VALUES ($1, 'active', 'vpn', 'vless')
			RETURNING id::text
		`, name).Scan(&serverIDs[index]); err != nil {
			t.Fatalf("create server %s: %v", name, err)
		}
		capabilities := `{"vpnCores":[{"type":"sing-box","state":"running"}],"routegate":{"schemaVersion":1,"vpnCoreAdapters":[{"core":"sing-box","protocol":"vless"}]}}`
		if _, err := pool.Exec(ctx, `
			INSERT INTO agents (
				server_id, hostname, os, arch, agent_version, protocol_version,
				status, token_hash, capabilities, registered_at, last_seen_at,
				runtime_load_1, runtime_logical_cpus
			) VALUES (
				$1::uuid, $2, 'linux', 'amd64', 'test', 1,
				'online', $3, $4::jsonb, now(), now(), 0.2, 2
			)
		`, serverIDs[index], name, name+"-token", capabilities); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
	}

	var groupID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO node_groups (name, selection_strategy)
		VALUES ('automatic selection integration', 'priority')
		RETURNING id::text
	`).Scan(&groupID); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO node_group_members (node_group_id, server_id, priority, weight)
		VALUES ($1::uuid, $2::uuid, 100, 100), ($1::uuid, $3::uuid, 10, 100)
	`, groupID, serverIDs[0], serverIDs[1]); err != nil {
		t.Fatalf("create members: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('automatic-selection-test', 'sing-box', 'Automatic selection test', 'active', $1::uuid)
		RETURNING id::text
	`, serverIDs[0]).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_node_groups (vpn_account_id, node_group_id)
		VALUES ($1::uuid, $2::uuid)
	`, accountID, groupID); err != nil {
		t.Fatalf("assign account node group: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_automatic_selection_policies (
			vpn_account_id, enabled, allow_degraded, cooldown_seconds
		) VALUES ($1::uuid, TRUE, FALSE, 300)
	`, accountID); err != nil {
		t.Fatalf("create account selection policy: %v", err)
	}

	repository := vpnaccounts.NewRepository(pool)
	preview, err := repository.PreviewAutomaticSelection(ctx, accountID)
	if err != nil {
		t.Fatalf("preview selection: %v", err)
	}
	if preview.Status != vpnaccounts.SelectionStatusSelected || preview.SelectedCandidate == nil || preview.SelectedCandidate.ServerID != serverIDs[1] || !preview.CanApply {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	result, err := repository.ApplyAutomaticSelection(ctx, accountID)
	if err != nil {
		t.Fatalf("apply selection: %v", err)
	}
	if !result.Changed || !result.ConfigDeploymentRequired || result.SelectedServerID != serverIDs[1] || len(result.AffectedServerIDs) != 2 {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	var assignedServerID string
	if err := pool.QueryRow(ctx, `SELECT server_id::text FROM vpn_accounts WHERE id = $1::uuid`, accountID).Scan(&assignedServerID); err != nil {
		t.Fatalf("read assignment: %v", err)
	}
	if assignedServerID != serverIDs[1] {
		t.Fatalf("assigned server = %s, want %s", assignedServerID, serverIDs[1])
	}
}
