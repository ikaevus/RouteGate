package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/servers"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// Clients must receive the parameters of the last Agent-confirmed apply.
// Saved-but-unapplied settings, failed applies and rollbacks must never hand
// clients parameters that the running node does not use.
func TestClientMaterialFollowsLastSuccessfullyAppliedConfig(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	var serverID, accountID, profilelessAccountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name, status, deployment_role, public_ip, vpn_protocol, vless_port, vless_flow, vless_network,
			reality_private_key, reality_public_key, reality_short_id, reality_server_name
		) VALUES (
			'applied-settings-test', 'active', 'vpn', '203.0.113.10', 'vless', 8443, 'xtls-rprx-vision', 'tcp',
			'private-key-one', 'public-key-one', '0123456789abcdef', 'www.microsoft.com'
		)
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, target := range []*string{&accountID, &profilelessAccountID} {
		if err := pool.QueryRow(ctx, `
			INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id, vless_uuid)
			VALUES ('acct-' || substr(md5(random()::text), 1, 8), 'sing-box', 'Applied settings', 'active', $1::uuid, gen_random_uuid())
			RETURNING id::text
		`, serverID).Scan(target); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}

	accounts := vpnaccounts.NewRepository(pool)
	serverRepo := servers.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))

	// First run: nothing has ever been applied, so the saved settings are not
	// running anywhere and no client link may be issued.
	if _, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID); err == nil ||
		!strings.Contains(err.Error(), vpnaccounts.ErrNodeConfigNotApplied.Error()) {
		t.Fatalf("first run must not issue client material, got %v", err)
	}

	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertVLESSLink(t, ctx, accounts, accountID, "8443", "www.microsoft.com", "public-key-one", "0123456789abcdef")

	// Save new client-facing parameters without applying them.
	newPort, newName, newShortID := 10443, "www.apple.com", "fedcba9876543210"
	if _, err := serverRepo.UpdateProtocolSettings(ctx, serverID, servers.UpdateProtocolSettingsInput{
		VLESSPort: &newPort, RealityServerName: &newName, RealityShortID: &newShortID,
	}); err != nil {
		t.Fatalf("save protocol settings: %v", err)
	}
	if _, err := serverRepo.UpdateRealityKeypair(ctx, serverID, servers.UpdateRealityKeypairInput{
		PrivateKey: "private-key-two", PublicKey: "public-key-two",
	}); err != nil {
		t.Fatalf("save Reality keypair: %v", err)
	}
	assertVLESSLink(t, ctx, accounts, accountID, "8443", "www.microsoft.com", "public-key-one", "0123456789abcdef")

	// A rendered but rejected version must not reach clients either.
	v2 := renderVersion(t, ctx, render, serverID)
	if v2 == v1 {
		t.Fatal("changed settings must render a new config version")
	}
	assertVLESSLink(t, ctx, accounts, accountID, "8443", "www.microsoft.com", "public-key-one", "0123456789abcdef")
	finishApply(t, ctx, pool, serverID, v2, "failed")
	assertVLESSLink(t, ctx, accounts, accountID, "8443", "www.microsoft.com", "public-key-one", "0123456789abcdef")

	// Only the Agent-confirmed apply releases the new parameters.
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertVLESSLink(t, ctx, accounts, accountID, "10443", "www.apple.com", "public-key-two", "fedcba9876543210")

	// Rolling back by re-applying the previous version restores its parameters.
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertVLESSLink(t, ctx, accounts, accountID, "8443", "www.microsoft.com", "public-key-one", "0123456789abcdef")

	// A saved node protocol switch stays invisible to an account whose client
	// profile row does not exist yet, until an apply confirms it.
	if _, err := pool.Exec(ctx, `UPDATE servers SET vpn_protocol = 'wireguard' WHERE id = $1::uuid`, serverID); err != nil {
		t.Fatalf("save protocol switch: %v", err)
	}
	protocol, err := accounts.GetActiveClientProtocol(ctx, profilelessAccountID)
	if err != nil {
		t.Fatalf("read active protocol: %v", err)
	}
	if protocol != vpnaccounts.ClientProtocolVLESS {
		t.Fatalf("unapplied node protocol switch reached clients: %q", protocol)
	}

	// Versions rendered before snapshots existed keep the previous behaviour
	// (live settings) until the next successful apply records a snapshot.
	if _, err := pool.Exec(ctx, `
		UPDATE servers SET vpn_protocol = 'vless' WHERE id = $1::uuid;
	`, serverID); err != nil {
		t.Fatalf("restore protocol: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE config_versions SET client_settings = NULL WHERE id = $1::uuid`, v1); err != nil {
		t.Fatalf("simulate legacy version: %v", err)
	}
	assertVLESSLink(t, ctx, accounts, accountID, "10443", "www.apple.com", "public-key-two", "fedcba9876543210")
}

func renderVersion(t *testing.T, ctx context.Context, render *configs.Service, serverID string) string {
	t.Helper()
	response, err := render.Render(ctx, serverID)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return response.ConfigVersion.ID
}

func finishApply(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID, versionID, status string) {
	t.Helper()
	var jobID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO config_apply_jobs (server_id, config_version_id, action, status, request_payload, result_payload)
		VALUES ($1::uuid, $2::uuid, 'apply', 'pending', '{}'::jsonb, '{}'::jsonb)
		RETURNING id::text
	`, serverID, versionID).Scan(&jobID); err != nil {
		t.Fatalf("create apply job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE config_apply_jobs SET status = $2, completed_at = now() WHERE id = $1::uuid
	`, jobID, status); err != nil {
		t.Fatalf("finish apply job as %s: %v", status, err)
	}
}

func assertVLESSLink(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, accountID, port, serverName, publicKey, shortID string) {
	t.Helper()
	connection, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID)
	if err != nil {
		t.Fatalf("build client connection: %v", err)
	}
	link := connection.VLESSLink
	for _, want := range []string{"@203.0.113.10:" + port + "?", "sni=" + serverName, "pbk=" + publicKey, "sid=" + shortID} {
		if !strings.Contains(link, want) {
			t.Fatalf("client link %q does not contain %q", link, want)
		}
	}
}
