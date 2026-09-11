package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestVpnAccountDevicesMigrationBackfillsExistingSubscriptionToken verifies
// migration 000149 (RG-116 Access & Devices) preserves existing subscription
// URLs: an account with an active token before the migration keeps that
// token active and gets exactly one backfilled device carrying over the
// account's prior client/device type, so the existing link keeps resolving
// after the upgrade.
func TestVpnAccountDevicesMigrationBackfillsExistingSubscriptionToken(t *testing.T) {
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
	preDeviceDir := copyMigrationsBefore(t, "../../migrations", "000149_vpn_account_devices.up.sql")
	if err := Migrate(ctx, pool, preDeviceDir, logger); err != nil {
		t.Fatalf("apply migrations through 000148: %v", err)
	}

	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status)
		VALUES ('rg116-legacy-fixture', 'sing-box', 'RG-116 legacy fixture', 'active')
		RETURNING id::text
	`).Scan(&accountID); err != nil {
		t.Fatalf("create legacy account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id, client_type, device_type)
		VALUES ($1::uuid, 'v2rayn', 'windows')
	`, accountID); err != nil {
		t.Fatalf("create legacy client profile: %v", err)
	}
	var tokenID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
		VALUES ($1::uuid, 'rg116-legacy-token-hash', 'active')
		RETURNING id::text
	`, accountID).Scan(&tokenID); err != nil {
		t.Fatalf("create legacy subscription token: %v", err)
	}

	// A second account with a client profile but no subscription token must
	// not get a backfilled device: it has no existing access to preserve.
	var noTokenAccountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status)
		VALUES ('rg116-no-token-fixture', 'sing-box', 'RG-116 no-token fixture', 'active')
		RETURNING id::text
	`).Scan(&noTokenAccountID); err != nil {
		t.Fatalf("create no-token account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id, client_type, device_type)
		VALUES ($1::uuid, 'hiddify', 'ios')
	`, noTokenAccountID); err != nil {
		t.Fatalf("create no-token client profile: %v", err)
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply vpn_account_devices migration: %v", err)
	}

	var deviceCount int
	var deviceID, deviceClientType, deviceDeviceType string
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) OVER (), id::text, client_type, device_type
		FROM vpn_account_devices
		WHERE vpn_account_id = $1::uuid
	`, accountID).Scan(&deviceCount, &deviceID, &deviceClientType, &deviceDeviceType); err != nil {
		t.Fatalf("read backfilled device: %v", err)
	}
	if deviceCount != 1 {
		t.Fatalf("backfilled device count = %d, want exactly 1", deviceCount)
	}
	if deviceClientType != "v2rayn" || deviceDeviceType != "windows" {
		t.Fatalf("backfilled device client/device type = %q/%q, want v2rayn/windows", deviceClientType, deviceDeviceType)
	}

	var linkedDeviceID, tokenStatus string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(device_id::text, ''), status FROM vpn_subscription_tokens WHERE id = $1::uuid
	`, tokenID).Scan(&linkedDeviceID, &tokenStatus); err != nil {
		t.Fatalf("read migrated token: %v", err)
	}
	if tokenStatus != "active" {
		t.Fatalf("existing token status = %q, want active (must keep resolving)", tokenStatus)
	}
	if linkedDeviceID != deviceID {
		t.Fatalf("existing token device_id = %q, want backfilled device %q", linkedDeviceID, deviceID)
	}

	var noTokenDeviceCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vpn_account_devices WHERE vpn_account_id = $1::uuid
	`, noTokenAccountID).Scan(&noTokenDeviceCount); err != nil {
		t.Fatalf("count devices for no-token account: %v", err)
	}
	if noTokenDeviceCount != 0 {
		t.Fatalf("no-token account device count = %d, want 0", noTokenDeviceCount)
	}

	// A second device on the SAME account can hold its own active token
	// concurrently: the one-active-token constraint is now per-device, not
	// per-account, so a compromised device's token can be rotated/revoked
	// without affecting the other device.
	var secondDeviceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_account_devices (vpn_account_id, name, client_type, device_type)
		VALUES ($1::uuid, 'Second device', 'hiddify', 'ios')
		RETURNING id::text
	`, accountID).Scan(&secondDeviceID); err != nil {
		t.Fatalf("create second device: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, device_id, token_hash, status)
		VALUES ($1::uuid, $2::uuid, 'rg116-second-device-token-hash', 'active')
	`, accountID, secondDeviceID); err != nil {
		t.Fatalf("issue token for second device while first device token is still active: %v", err)
	}

	// But a single device still cannot hold two active tokens at once.
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, device_id, token_hash, status)
		VALUES ($1::uuid, $2::uuid, 'rg116-second-device-duplicate-token-hash', 'active')
	`, accountID, secondDeviceID); err == nil {
		t.Fatal("expected duplicate active token for the same device to violate the per-device uniqueness index")
	}
}
