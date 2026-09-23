package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/configs"
)

func TestEquivalentRenderAfterProtocolSetEditGetsNewActivationTimestamp(t *testing.T) {
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

	var serverID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (name, status) VALUES ('protocol-activation-test', 'active')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id)
		VALUES ('protocol-activation-test', 'sing-box', 'Protocol test', 'active', $1::uuid)
		RETURNING id::text
	`, serverID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO vpn_client_profiles (vpn_account_id) VALUES ($1::uuid)`, accountID); err != nil {
		t.Fatalf("create client profile: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_account_protocols (vpn_account_id, protocol, desired_enabled, active_enabled)
		VALUES ($1::uuid, 'hysteria2', TRUE, FALSE)
	`, accountID); err != nil {
		t.Fatalf("save desired Hysteria2: %v", err)
	}

	repo := configs.NewRepository(pool)
	input := configs.CreateConfigVersionInput{
		ServerID: serverID, Status: configs.StatusValidated,
		ConfigHash: "identical-render", RenderedConfig: configs.RenderedConfig{},
	}
	first, err := repo.CreateConfigVersion(ctx, input)
	if err != nil {
		t.Fatalf("render first version: %v", err)
	}
	// A repeated save happens after the initial version was rendered. It must
	// not be activated by an apply of that old version.
	if _, err := pool.Exec(ctx, `
		UPDATE config_versions SET created_at = now() - interval '1 hour' WHERE id = $1::uuid
	`, first.ID); err != nil {
		t.Fatalf("age first version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE vpn_account_protocols SET updated_at = now()
		WHERE vpn_account_id = $1::uuid AND protocol = 'hysteria2'
	`, accountID); err != nil {
		t.Fatalf("repeat desired protocol save: %v", err)
	}

	second, err := repo.CreateConfigVersion(ctx, input)
	if err != nil {
		t.Fatalf("render repeated preference: %v", err)
	}
	if second.ID == first.ID || !second.CreatedAt.After(first.CreatedAt) {
		t.Fatalf("reused version older than preference: first=%+v second=%+v", first, second)
	}
	var jobID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO config_apply_jobs (
			server_id, config_version_id, action, status, request_payload, result_payload
		) VALUES ($1::uuid, $2::uuid, 'apply', 'pending', '{}'::jsonb, '{}'::jsonb)
		RETURNING id::text
	`, serverID, second.ID).Scan(&jobID); err != nil {
		t.Fatalf("create apply job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE config_apply_jobs SET status = 'succeeded', completed_at = now()
		WHERE id = $1::uuid
	`, jobID); err != nil {
		t.Fatalf("confirm apply: %v", err)
	}
	var active bool
	if err := pool.QueryRow(ctx, `
		SELECT active_enabled FROM vpn_account_protocols
		WHERE vpn_account_id = $1::uuid AND protocol = 'hysteria2'
	`, accountID).Scan(&active); err != nil {
		t.Fatalf("read activated protocol: %v", err)
	}
	if !active {
		t.Fatal("successful apply did not activate the saved Hysteria2 protocol")
	}
}
