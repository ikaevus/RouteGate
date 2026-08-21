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

func TestAutomaticSelectionSkipsHybridNodeForExplicitHysteria2(t *testing.T) {
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

	type serverFixture struct {
		name     string
		role     string
		priority int
	}
	fixtures := []serverFixture{
		{name: "hysteria-current", role: "vpn", priority: 100},
		{name: "hysteria-hybrid-preferred", role: "hybrid", priority: 10},
		{name: "hysteria-dedicated-target", role: "vpn", priority: 20},
	}
	serverIDs := make([]string, len(fixtures))
	hysteriaCapabilities := `{"vpnCores":[{"type":"hysteria","state":"running"}],"routegate":{"schemaVersion":1,"vpnCoreAdapters":[{"core":"hysteria","protocol":"hysteria2"}]}}`
	for index, fixture := range fixtures {
		if err := pool.QueryRow(ctx, `
			INSERT INTO servers (name, status, deployment_role, vpn_protocol)
			VALUES ($1, 'active', $2, 'vless')
			RETURNING id::text
		`, fixture.name, fixture.role).Scan(&serverIDs[index]); err != nil {
			t.Fatalf("create server %s: %v", fixture.name, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO agents (
				server_id, hostname, os, arch, agent_version, protocol_version,
				status, token_hash, capabilities, registered_at, last_seen_at,
				runtime_load_1, runtime_logical_cpus
			) VALUES (
				$1::uuid, $2, 'linux', 'amd64', 'test', 1,
				'online', $3, $4::jsonb, now(), now(), 0.2, 2
			)
		`, serverIDs[index], fixture.name, fixture.name+"-token", hysteriaCapabilities); err != nil {
			t.Fatalf("create agent %s: %v", fixture.name, err)
		}
	}

	var groupID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO node_groups (name, selection_strategy)
		VALUES ('Hysteria topology selection', 'priority')
		RETURNING id::text
	`).Scan(&groupID); err != nil {
		t.Fatalf("create node group: %v", err)
	}
	for index, fixture := range fixtures {
		if _, err := pool.Exec(ctx, `
			INSERT INTO node_group_members (node_group_id, server_id, priority, weight)
			VALUES ($1::uuid, $2::uuid, $3, 100)
		`, groupID, serverIDs[index], fixture.priority); err != nil {
			t.Fatalf("create node group member %s: %v", fixture.name, err)
		}
	}

	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('hysteria-topology-account', 'sing-box', 'Hysteria topology account', 'active', $1::uuid)
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
	if preview.SelectedCandidate == nil {
		t.Fatalf("preview did not select a topology-compatible candidate: %+v", preview)
	}
	if preview.SelectedCandidate.ServerID != serverIDs[2] {
		t.Fatalf("selected server = %s, want dedicated VPN target %s; decision=%+v", preview.SelectedCandidate.ServerID, serverIDs[2], preview)
	}
	if preview.SelectedCandidate.Protocol != vpnaccounts.ClientProtocolHysteria2 {
		t.Fatalf("selected protocol = %q, want hysteria2", preview.SelectedCandidate.Protocol)
	}
	if preview.EligibleCandidates != 2 {
		t.Fatalf("eligible candidates = %d, want 2 dedicated VPN nodes", preview.EligibleCandidates)
	}

	result, err := repository.ApplyAutomaticSelection(ctx, accountID)
	if err != nil {
		t.Fatalf("apply automatic selection: %v", err)
	}
	if !result.Changed || result.SelectedServerID != serverIDs[2] {
		t.Fatalf("unexpected topology-aware apply result: %+v", result)
	}
}
