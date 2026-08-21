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

func TestAutomaticSelectionUsesExplicitAccountProtocolForCandidateEvidence(t *testing.T) {
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
	for index, name := range []string{"default-vless-preferred", "explicit-hysteria-capable"} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role, vpn_protocol)
			VALUES ($1, 'active', 'vpn', 'vless')
			RETURNING id::text
		`, name).Scan(&serverIDs[index]); err != nil {
			t.Fatalf("create server %s: %v", name, err)
		}
	}

	vlessCapabilities := `{"vpnCores":[{"type":"sing-box","state":"running"}],"routegate":{"schemaVersion":1,"vpnCoreAdapters":[{"core":"sing-box","protocol":"vless"}]}}`
	hysteriaCapabilities := `{"vpnCores":[{"type":"hysteria","state":"running"}],"routegate":{"schemaVersion":1,"vpnCoreAdapters":[{"core":"hysteria","protocol":"hysteria2"}]}}`
	for index, capabilities := range []string{vlessCapabilities, hysteriaCapabilities} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO agents (
				server_id, hostname, os, arch, agent_version, protocol_version,
				status, token_hash, capabilities, registered_at, last_seen_at,
				runtime_load_1, runtime_logical_cpus
			) VALUES (
				$1::uuid, $2, 'linux', 'amd64', 'test', 1,
				'online', $3, $4::jsonb, now(), now(), 0.2, 2
			)
		`, serverIDs[index], "protocol-aware-agent", serverIDs[index]+"-token", capabilities); err != nil {
			t.Fatalf("create agent %d: %v", index, err)
		}
	}

	var groupID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO node_groups (name, selection_strategy)
		VALUES ('protocol-aware selection', 'priority')
		RETURNING id::text
	`).Scan(&groupID); err != nil {
		t.Fatalf("create node group: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO node_group_members (node_group_id, server_id, priority, weight)
		VALUES ($1::uuid, $2::uuid, 10, 100), ($1::uuid, $3::uuid, 20, 100)
	`, groupID, serverIDs[0], serverIDs[1]); err != nil {
		t.Fatalf("create node group members: %v", err)
	}

	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('protocol-aware-account', 'sing-box', 'Protocol-aware account', 'active', $1::uuid)
		RETURNING id::text
	`, serverIDs[0]).Scan(&accountID); err != nil {
		t.Fatalf("create VPN account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id, protocol)
		VALUES ($1::uuid, 'hysteria2')
	`, accountID); err != nil {
		t.Fatalf("create explicit Hysteria2 client profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_node_groups (vpn_account_id, node_group_id)
		VALUES ($1::uuid, $2::uuid)
	`, accountID, groupID); err != nil {
		t.Fatalf("assign node group: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_automatic_selection_policies (
			vpn_account_id, enabled, allow_degraded, cooldown_seconds
		) VALUES ($1::uuid, TRUE, FALSE, 300)
	`, accountID); err != nil {
		t.Fatalf("create automatic-selection policy: %v", err)
	}

	repository := vpnaccounts.NewRepository(pool)
	preview, err := repository.PreviewAutomaticSelection(ctx, accountID)
	if err != nil {
		t.Fatalf("preview automatic selection: %v", err)
	}
	if preview.Status != vpnaccounts.SelectionStatusSelected {
		t.Fatalf("preview status = %q, want %q; decision=%+v", preview.Status, vpnaccounts.SelectionStatusSelected, preview)
	}
	if preview.SelectedCandidate == nil {
		t.Fatalf("preview did not select an explicit-protocol candidate: %+v", preview)
	}
	if preview.SelectedCandidate.ServerID != serverIDs[1] {
		t.Fatalf("selected server = %s, want explicit-protocol-capable server %s", preview.SelectedCandidate.ServerID, serverIDs[1])
	}
	if preview.SelectedCandidate.Protocol != vpnaccounts.ClientProtocolHysteria2 {
		t.Fatalf("selected protocol = %q, want %q", preview.SelectedCandidate.Protocol, vpnaccounts.ClientProtocolHysteria2)
	}
	if preview.EligibleCandidates != 1 || !preview.CanApply {
		t.Fatalf("unexpected protocol-aware preview: %+v", preview)
	}

	result, err := repository.ApplyAutomaticSelection(ctx, accountID)
	if err != nil {
		t.Fatalf("apply automatic selection: %v", err)
	}
	if !result.Changed || !result.ConfigDeploymentRequired || result.SelectedServerID != serverIDs[1] {
		t.Fatalf("unexpected protocol-aware apply result: %+v", result)
	}
	if result.Decision.SelectedCandidate == nil || result.Decision.SelectedCandidate.Protocol != vpnaccounts.ClientProtocolHysteria2 {
		t.Fatalf("apply decision lost explicit protocol: %+v", result.Decision)
	}

	var assignedServerID string
	if err := pool.QueryRow(ctx, `SELECT server_id::text FROM vpn_accounts WHERE id = $1::uuid`, accountID).Scan(&assignedServerID); err != nil {
		t.Fatalf("read final account assignment: %v", err)
	}
	if assignedServerID != serverIDs[1] {
		t.Fatalf("assigned server = %s, want %s", assignedServerID, serverIDs[1])
	}
}
