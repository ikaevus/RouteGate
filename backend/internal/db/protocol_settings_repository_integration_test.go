package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/servers"
)

func TestProtocolSettingsRepositoryUpdatesSecondaryHysteria2(t *testing.T) {
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
		INSERT INTO servers (name, hostname, status, deployment_role, vpn_protocol)
		VALUES ('us.routegate.org', 'us.routegate.org', 'active', 'hybrid', 'vless')
		RETURNING id::text
	`).Scan(&serverID); err != nil {
		t.Fatalf("create Hybrid Node: %v", err)
	}

	port := 443
	domain := "hy2.us.routegate.org"
	email := "ikaevus@gmail.com"
	masqueradeURL := "https://www.cloudflare.com/"
	settings, err := servers.NewRepository(pool).UpdateProtocolSettings(ctx, serverID, servers.UpdateProtocolSettingsInput{
		Hysteria2Port:          &port,
		Hysteria2Domain:        &domain,
		Hysteria2ACMEEmail:     &email,
		Hysteria2MasqueradeURL: &masqueradeURL,
	})
	if err != nil {
		t.Fatalf("update secondary Hysteria2 settings: %v", err)
	}
	if settings.Protocol != "vless" {
		t.Fatalf("node default protocol = %q, want vless", settings.Protocol)
	}
	if settings.Hysteria2Port != port || settings.Hysteria2Domain != domain || settings.Hysteria2ACMEEmail != email || settings.Hysteria2MasqueradeURL != masqueradeURL {
		t.Fatalf("unexpected Hysteria2 settings: %+v", settings)
	}
}

func TestRecommendedRealityWriteIsAtomic(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `INSERT INTO servers (name, status, deployment_role)
		VALUES ('Reality test node', 'active', 'vpn') RETURNING id::text`).Scan(&serverID); err != nil {
		t.Fatalf("create VPN Node: %v", err)
	}
	repository := servers.NewRepository(pool)
	initial := servers.RecommendedRealityInput{PrivateKey: "private-1", PublicKey: "public-1", ShortID: "0123456789abcdef", ServerName: "www.example.org"}
	settings, err := repository.ConfigureRecommendedReality(ctx, serverID, initial)
	if err != nil {
		t.Fatalf("configure Reality: %v", err)
	}
	if settings.Protocol != "vless" || settings.VLESSPort != 8443 || settings.VLESSFlow != "xtls-rprx-vision" || settings.RealityServerName != initial.ServerName {
		t.Fatalf("unexpected recommended settings: %+v", settings)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE servers ADD CONSTRAINT reject_reality_test_name
		CHECK (reality_server_name IS DISTINCT FROM 'reject.example.org')`); err != nil {
		t.Fatalf("add test rejection constraint: %v", err)
	}
	if _, err := repository.ConfigureRecommendedReality(ctx, serverID, servers.RecommendedRealityInput{
		PrivateKey: "private-2", PublicKey: "public-2", ShortID: "fedcba9876543210", ServerName: "reject.example.org",
	}); err == nil {
		t.Fatal("expected rejected Reality update")
	}
	var privateKey, publicKey, shortID, serverName string
	if err := pool.QueryRow(ctx, `SELECT reality_private_key, reality_public_key, reality_short_id, reality_server_name
		FROM servers WHERE id = $1::uuid`, serverID).Scan(&privateKey, &publicKey, &shortID, &serverName); err != nil {
		t.Fatalf("read Reality settings after rejected write: %v", err)
	}
	if privateKey != initial.PrivateKey || publicKey != initial.PublicKey || shortID != initial.ShortID || serverName != initial.ServerName {
		t.Fatal("rejected update changed some Reality settings")
	}
}
