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
