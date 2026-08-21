package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin account blocker: %v", err)
	}
	defer blocker.Rollback(ctx)
	var lockedAccountID string
	if err := blocker.QueryRow(ctx, `
		SELECT id::text FROM vpn_accounts WHERE id = $1::uuid FOR UPDATE
	`, accountID).Scan(&lockedAccountID); err != nil {
		t.Fatalf("lock account before concurrent apply: %v", err)
	}

	applyPoolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse apply pool config: %v", err)
	}
	applyPoolConfig.MaxConns = 1
	applyPoolConfig.ConnConfig.RuntimeParams["application_name"] = "routegate-automatic-selection-apply-test"
	applyPool, err := pgxpool.NewWithConfig(ctx, applyPoolConfig)
	if err != nil {
		t.Fatalf("create apply pool: %v", err)
	}
	defer applyPool.Close()
	if err := applyPool.Ping(ctx); err != nil {
		t.Fatalf("ping apply pool: %v", err)
	}

	type applyResult struct {
		response vpnaccounts.AutomaticSelectionApplyResponse
		err      error
	}
	applyResultChannel := make(chan applyResult, 1)
	go func() {
		response, applyErr := vpnaccounts.NewRepository(applyPool).ApplyAutomaticSelection(ctx, accountID)
		applyResultChannel <- applyResult{response: response, err: applyErr}
	}()

	deadline := time.Now().Add(5 * time.Second)
	blockedAfterAccountLock := false
	for time.Now().Before(deadline) {
		var waitEventType, query string
		err := pool.QueryRow(ctx, `
			SELECT COALESCE(wait_event_type, ''), query
			FROM pg_stat_activity
			WHERE application_name = 'routegate-automatic-selection-apply-test'
			  AND state = 'active'
		`).Scan(&waitEventType, &query)
		if err == nil && waitEventType == "Lock" && strings.Contains(query, "FOR UPDATE OF a") {
			blockedAfterAccountLock = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blockedAfterAccountLock {
		t.Fatal("automatic selection apply did not block while acquiring the account lock")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE vpn_account_automatic_selection_policies
		SET enabled = FALSE
		WHERE vpn_account_id = $1::uuid
	`, accountID); err != nil {
		t.Fatalf("disable selection while apply waits: %v", err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release account blocker: %v", err)
	}
	select {
	case concurrentResult := <-applyResultChannel:
		if !errors.Is(concurrentResult.err, vpnaccounts.ErrAutomaticSelectionDisabled) {
			t.Fatalf("concurrent apply error = %v, want automatic selection disabled; response=%+v", concurrentResult.err, concurrentResult.response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent automatic selection apply")
	}
	var assignedAfterRejectedApply string
	if err := pool.QueryRow(ctx, `SELECT server_id::text FROM vpn_accounts WHERE id = $1::uuid`, accountID).Scan(&assignedAfterRejectedApply); err != nil {
		t.Fatalf("read assignment after rejected apply: %v", err)
	}
	if assignedAfterRejectedApply != serverIDs[0] {
		t.Fatalf("assignment changed after stale apply: got %s, want %s", assignedAfterRejectedApply, serverIDs[0])
	}
	if _, err := pool.Exec(ctx, `
		UPDATE vpn_account_automatic_selection_policies
		SET enabled = TRUE
		WHERE vpn_account_id = $1::uuid
	`, accountID); err != nil {
		t.Fatalf("re-enable selection: %v", err)
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

	var lastSelectedAtBefore, lastSelectedAtAfter time.Time
	var lastSelectedServerIDBefore, lastSelectedServerIDAfter string
	if err := pool.QueryRow(ctx, `
		SELECT last_selected_at, last_selected_server_id::text
		FROM vpn_account_automatic_selection_policies
		WHERE vpn_account_id = $1::uuid
	`, accountID).Scan(&lastSelectedAtBefore, &lastSelectedServerIDBefore); err != nil {
		t.Fatalf("read selection history before idempotent assignment: %v", err)
	}
	if _, err := repository.AssignNodeGroup(ctx, accountID, groupID); err != nil {
		t.Fatalf("repeat the same node-group assignment: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT last_selected_at, last_selected_server_id::text
		FROM vpn_account_automatic_selection_policies
		WHERE vpn_account_id = $1::uuid
	`, accountID).Scan(&lastSelectedAtAfter, &lastSelectedServerIDAfter); err != nil {
		t.Fatalf("read selection history after idempotent assignment: %v", err)
	}
	if !lastSelectedAtAfter.Equal(lastSelectedAtBefore) || lastSelectedServerIDAfter != lastSelectedServerIDBefore {
		t.Fatalf(
			"same node-group assignment reset selection history: before=(%s, %s) after=(%s, %s)",
			lastSelectedAtBefore, lastSelectedServerIDBefore, lastSelectedAtAfter, lastSelectedServerIDAfter,
		)
	}
}
