package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfigApplyTriggerRepairActivatesPendingProtocolSets(t *testing.T) {
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

	preRepairDir := copyMigrationsBefore(t, "../../migrations", "000155_config_apply_trigger_invariant_repair.up.sql")
	if err := Migrate(ctx, pool, preRepairDir, logger); err != nil {
		t.Fatalf("apply migrations before 000155: %v", err)
	}
	// Physical drift observed on a production-like host: the function exists
	// but the trigger invoking it was lost.
	if _, err := pool.Exec(ctx, `DROP TRIGGER config_apply_jobs_mark_version_applied ON config_apply_jobs`); err != nil {
		t.Fatalf("simulate trigger drift: %v", err)
	}

	var serverID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status) VALUES ('trigger-repair-test', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('trigger-repair-test', 'sing-box', 'Trigger repair', 'active', $1::uuid)
		RETURNING id::text
	`, serverID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id) VALUES ($1::uuid);
	`, accountID); err != nil {
		t.Fatalf("create client profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_protocols (vpn_account_id, protocol, desired_enabled, active_enabled, updated_at)
		VALUES
			($1::uuid, 'vless', TRUE, TRUE, now() - interval '2 hours'),
			($1::uuid, 'hysteria2', TRUE, FALSE, now() - interval '1 hour')
	`, accountID); err != nil {
		t.Fatalf("save desired protocols: %v", err)
	}

	applyVersion := func(label string) {
		t.Helper()
		var versionID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO config_versions (server_id, version, config_hash, status, rendered_config)
			VALUES ($1::uuid, (SELECT COALESCE(MAX(version), 0) + 1 FROM config_versions WHERE server_id = $1::uuid), $2, 'validated', '{}'::jsonb)
			RETURNING id::text
		`, serverID, label).Scan(&versionID); err != nil {
			t.Fatalf("render %s: %v", label, err)
		}
		var jobID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO config_apply_jobs (server_id, config_version_id, action, status, request_payload, result_payload)
			VALUES ($1::uuid, $2::uuid, 'apply', 'pending', '{}'::jsonb, '{}'::jsonb)
			RETURNING id::text
		`, serverID, versionID).Scan(&jobID); err != nil {
			t.Fatalf("create apply job %s: %v", label, err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE config_apply_jobs SET status = 'succeeded', completed_at = now() WHERE id = $1::uuid
		`, jobID); err != nil {
			t.Fatalf("confirm apply %s: %v", label, err)
		}
	}

	applyVersion("before-repair")
	if active := protocolActive(t, ctx, pool, accountID, "hysteria2"); active {
		t.Fatal("drifted schema unexpectedly activated Hysteria2; the test no longer reproduces the bug")
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply repair migration: %v", err)
	}
	if active := protocolActive(t, ctx, pool, accountID, "hysteria2"); !active {
		t.Fatal("repair did not activate the protocol covered by the last successful apply")
	}

	// New desired protocols must again be promoted by a later successful apply.
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_protocols (vpn_account_id, protocol, desired_enabled, active_enabled, updated_at)
		VALUES ($1::uuid, 'mtproto', TRUE, FALSE, now() - interval '1 minute')
	`, accountID); err != nil {
		t.Fatalf("save MTProto: %v", err)
	}
	applyVersion("after-repair")
	if active := protocolActive(t, ctx, pool, accountID, "mtproto"); !active {
		t.Fatal("restored trigger did not activate MTProto after a successful apply")
	}
}

func protocolActive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID, protocol string) bool {
	t.Helper()
	var active bool
	if err := pool.QueryRow(ctx, `
		SELECT active_enabled FROM vpn_account_protocols
		WHERE vpn_account_id = $1::uuid AND protocol = $2
	`, accountID, protocol).Scan(&active); err != nil {
		t.Fatalf("read %s activation: %v", protocol, err)
	}
	return active
}
