package db

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/servers"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type realityTestKey struct{ private, public string }

func newRealityTestKey(t *testing.T) realityTestKey {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return realityTestKey{
		private: base64.RawURLEncoding.EncodeToString(key.Bytes()),
		public:  base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
	}
}

type expectedVLESSLink struct{ port, serverName, publicKey, shortID string }

func setupAppliedSettingsTest(t *testing.T) (context.Context, *pgxpool.Pool, func()) {
	t.Helper()
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ctx, pool, func() { pool.Close(); cancel() }
}

func createAppliedSettingsServer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key realityTestKey) (string, string, string) {
	t.Helper()
	var serverID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name, status, deployment_role, public_ip, vpn_protocol, vless_port, vless_flow, vless_network,
			reality_private_key, reality_public_key, reality_short_id, reality_server_name
		) VALUES (
			'applied-settings-test', 'active', 'vpn', '203.0.113.10', 'vless', 8443, 'xtls-rprx-vision', 'tcp',
			$1, $2, '0123456789abcdef', 'www.microsoft.com'
		)
		RETURNING id::text
	`, key.private, key.public).Scan(&serverID); err != nil {
		t.Fatalf("create server: %v", err)
	}
	accounts := make([]string, 2)
	for i := range accounts {
		if err := pool.QueryRow(ctx, `
			INSERT INTO vpn_accounts (username, protocol, display_name, status, server_id, vless_uuid)
			VALUES ('acct-' || substr(md5(random()::text), 1, 8), 'sing-box', 'Applied settings', 'active', $1::uuid, gen_random_uuid())
			RETURNING id::text
		`, serverID).Scan(&accounts[i]); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}
	return serverID, accounts[0], accounts[1]
}

// Clients must receive the parameters of the last Agent-confirmed apply.
// Saved-but-unapplied settings, failed applies and rollbacks must never hand
// clients parameters that the running node does not use.
func TestClientMaterialFollowsLastSuccessfullyAppliedConfig(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	keyOne, keyTwo := newRealityTestKey(t), newRealityTestKey(t)
	serverID, accountID, profilelessAccountID := createAppliedSettingsServer(t, ctx, pool, keyOne)

	accounts := vpnaccounts.NewRepository(pool)
	serverRepo := servers.NewRepository(pool)
	render := configs.NewService(configs.NewRepository(pool))
	applied := expectedVLESSLink{"8443", "www.microsoft.com", keyOne.public, "0123456789abcdef"}

	// First run: nothing has ever been applied, so the saved settings are not
	// running anywhere and no client link may be issued.
	if _, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID); err == nil ||
		!strings.Contains(err.Error(), vpnaccounts.ErrNodeConfigNotApplied.Error()) {
		t.Fatalf("first run must not issue client material, got %v", err)
	}

	v1 := renderVersion(t, ctx, render, serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertVLESSLink(t, ctx, accounts, accountID, applied)

	// Save new client-facing parameters without applying them.
	newPort, newName, newShortID := 10443, "www.apple.com", "fedcba9876543210"
	if _, err := serverRepo.UpdateProtocolSettings(ctx, serverID, servers.UpdateProtocolSettingsInput{
		VLESSPort: &newPort, RealityServerName: &newName, RealityShortID: &newShortID,
	}); err != nil {
		t.Fatalf("save protocol settings: %v", err)
	}
	if _, err := serverRepo.UpdateRealityKeypair(ctx, serverID, servers.UpdateRealityKeypairInput{
		PrivateKey: keyTwo.private, PublicKey: keyTwo.public,
	}); err != nil {
		t.Fatalf("save Reality keypair: %v", err)
	}
	assertVLESSLink(t, ctx, accounts, accountID, applied)

	// A rendered but rejected version must not reach clients either.
	v2 := renderVersion(t, ctx, render, serverID)
	if v2 == v1 {
		t.Fatal("changed settings must render a new config version")
	}
	assertVLESSLink(t, ctx, accounts, accountID, applied)
	finishApply(t, ctx, pool, serverID, v2, "failed")
	assertVLESSLink(t, ctx, accounts, accountID, applied)

	// A transport the node never serves ("udp" passes the database check but
	// is not a VLESS transport) must not reach clients, before or after apply.
	if _, err := pool.Exec(ctx, `UPDATE servers SET vless_network = 'udp' WHERE id = $1::uuid`, serverID); err != nil {
		t.Fatalf("save unsupported transport: %v", err)
	}
	assertVLESSLink(t, ctx, accounts, accountID, applied)

	// Only the Agent-confirmed apply releases the new parameters, and clients
	// keep type=tcp because that is what the node serves.
	finishApply(t, ctx, pool, serverID, v2, "succeeded")
	assertVLESSLink(t, ctx, accounts, accountID, expectedVLESSLink{"10443", "www.apple.com", keyTwo.public, "fedcba9876543210"})

	// Rolling back by re-applying the previous version restores its parameters.
	finishApply(t, ctx, pool, serverID, v1, "succeeded")
	assertVLESSLink(t, ctx, accounts, accountID, applied)

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
}

// Config versions rendered before migration 000156 carry no snapshot. The
// Manager-start backfill derives one from the version's own rendered config,
// so the saved-but-unapplied values that differ from the running node stop
// reaching clients without changing any node or subscription token.
func TestLegacyAppliedVersionsGetSnapshotsFromTheirRenderedConfig(t *testing.T) {
	ctx, pool, done := setupAppliedSettingsTest(t)
	defer done()
	keyOne, keyTwo := newRealityTestKey(t), newRealityTestKey(t)
	serverID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, keyOne)

	accounts := vpnaccounts.NewRepository(pool)
	configRepo := configs.NewRepository(pool)
	v1 := renderVersion(t, ctx, configs.NewService(configRepo), serverID)
	finishApply(t, ctx, pool, serverID, v1, "succeeded")

	// Recreate the pre-000156 state: an applied version without a snapshot and
	// newer, unapplied settings saved for the node.
	if _, err := pool.Exec(ctx, `UPDATE config_versions SET client_settings = NULL`); err != nil {
		t.Fatalf("simulate legacy versions: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE servers
		SET reality_server_name = 'nx.routegate.invalid', reality_private_key = $2, reality_public_key = $3,
		    vless_network = 'udp', vless_port = 10443
		WHERE id = $1::uuid
	`, serverID, keyTwo.private, keyTwo.public); err != nil {
		t.Fatalf("save unapplied settings: %v", err)
	}
	// Without a snapshot the node falls back to the saved settings (the
	// original defect), which the backfill must correct.
	assertVLESSLinkTransport(t, ctx, accounts, accountID, expectedVLESSLink{"10443", "nx.routegate.invalid", keyTwo.public, "0123456789abcdef"}, "udp")

	filled, failures, err := configRepo.BackfillClientSettings(ctx)
	if err != nil || len(failures) != 0 || filled != 1 {
		t.Fatalf("backfill filled=%d failures=%+v err=%v", filled, failures, err)
	}
	assertVLESSLink(t, ctx, accounts, accountID, expectedVLESSLink{"8443", "www.microsoft.com", keyOne.public, "0123456789abcdef"})

	// The backfill is idempotent and never overwrites an existing snapshot.
	if filled, failures, err := configRepo.BackfillClientSettings(ctx); err != nil || filled != 0 || len(failures) != 0 {
		t.Fatalf("second backfill filled=%d failures=%+v err=%v", filled, failures, err)
	}

	// A legacy version whose rendered config cannot be derived is reported as
	// active and keeps its previous behaviour instead of cutting clients off.
	if _, err := pool.Exec(ctx, `
		UPDATE config_versions
		SET client_settings = NULL,
		    rendered_config = jsonb_set(rendered_config, '{wireGuard}', '"[Interface]\nPrivateKey = broken\n"')
		WHERE id = $1::uuid
	`, v1); err != nil {
		t.Fatalf("corrupt legacy version: %v", err)
	}
	filled, failures, err = configRepo.BackfillClientSettings(ctx)
	if err != nil || filled != 0 || len(failures) != 1 || !failures[0].Active || failures[0].VersionID != v1 {
		t.Fatalf("underivable active version must be reported: filled=%d failures=%+v err=%v", filled, failures, err)
	}
	if _, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID); err != nil {
		t.Fatalf("underivable legacy version must not cut clients off: %v", err)
	}
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

func assertVLESSLink(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, accountID string, want expectedVLESSLink) {
	t.Helper()
	assertVLESSLinkTransport(t, ctx, accounts, accountID, want, "tcp")
}

func assertVLESSLinkTransport(t *testing.T, ctx context.Context, accounts *vpnaccounts.Repository, accountID string, want expectedVLESSLink, transport string) {
	t.Helper()
	connection, err := vpnaccounts.BuildClientConnection(ctx, accounts, accountID)
	if err != nil {
		t.Fatalf("build client connection: %v", err)
	}
	link, err := url.Parse(connection.VLESSLink)
	if err != nil {
		t.Fatalf("parse client link: %v", err)
	}
	query := link.Query()
	if link.Host != "203.0.113.10:"+want.port || query.Get("sni") != want.serverName || query.Get("pbk") != want.publicKey ||
		query.Get("sid") != want.shortID || query.Get("type") != transport {
		t.Fatalf("client link %q, want port=%s sni=%s pbk=%s sid=%s type=%s",
			connection.VLESSLink, want.port, want.serverName, want.publicKey, want.shortID, transport)
	}
}
