package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/portal"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// TestMigratedLegacyTokenRotationSemantics is the item-1 regression test: a
// pre-RG-116 active subscription token, carried through migration 000149,
// must keep behaving exactly like "the account's subscription" for
// legacy/account-level and Portal rotation - both of which revoke only
// device_id IS NULL tokens - while remaining fully independent of any real
// RG-116 device tokens created afterward.
func TestMigratedLegacyTokenRotationSemantics(t *testing.T) {
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

	// Seed a pre-RG-116 account with an active subscription token, exactly
	// as it would have existed before this PR.
	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status)
		VALUES ('rg116-migrated-rotate-fixture', 'sing-box', 'RG-116 migrated rotate fixture', 'active')
		RETURNING id::text
	`).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	preUpgradeHash := vpnaccounts.HashSubscriptionToken("pre-upgrade-raw-token")
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
		VALUES ($1::uuid, $2, 'active')
	`, accountID, preUpgradeHash); err != nil {
		t.Fatalf("create pre-upgrade subscription token: %v", err)
	}

	// Now bring the schema up to the current migration, including 000149's
	// backfill.
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	accounts := vpnaccounts.NewRepository(pool)
	portalRepo := portal.NewRepository(pool)

	// 1. The pre-RG-116 token survives migration unchanged and active.
	var preUpgradeStatus string
	var preUpgradeDeviceID string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(device_id::text, '') FROM vpn_subscription_tokens WHERE token_hash = $1
	`, preUpgradeHash).Scan(&preUpgradeStatus, &preUpgradeDeviceID); err != nil {
		t.Fatalf("read pre-upgrade token after migration: %v", err)
	}
	if preUpgradeStatus != "active" {
		t.Fatalf("pre-upgrade token status = %q, want active", preUpgradeStatus)
	}
	if preUpgradeDeviceID != "" {
		t.Fatalf("pre-upgrade token device_id = %q, want empty (must remain the legacy credential)", preUpgradeDeviceID)
	}

	// 2. Its /sub/<token> URL still resolves after migration (via the real
	// FindActiveSubscriptionTokenByHash public-lookup path).
	resolved, err := accounts.FindActiveSubscriptionTokenByHash(ctx, preUpgradeHash)
	if err != nil {
		t.Fatalf("pre-upgrade token must still resolve after migration: %v", err)
	}
	if resolved.VPNAccountID != accountID {
		t.Fatalf("resolved pre-upgrade token account = %q, want %q", resolved.VPNAccountID, accountID)
	}

	// Create two genuine RG-116 devices with their own active tokens, to
	// prove they are untouched by everything that follows.
	deviceA, err := accounts.CreateDevice(ctx, vpnaccounts.CreateDeviceInput{VPNAccountID: accountID, Name: "Device A", ClientType: vpnaccounts.ClientTypeHiddify, DeviceType: vpnaccounts.DevicePlatformIOS})
	if err != nil {
		t.Fatalf("create device A: %v", err)
	}
	deviceAHash := vpnaccounts.HashSubscriptionToken("device-a-raw-token")
	if _, err := accounts.CreateDeviceSubscriptionToken(ctx, deviceA.ID, deviceAHash, nil); err != nil {
		t.Fatalf("issue device A token: %v", err)
	}
	deviceB, err := accounts.CreateDevice(ctx, vpnaccounts.CreateDeviceInput{VPNAccountID: accountID, Name: "Device B", ClientType: vpnaccounts.ClientTypeV2RayN, DeviceType: vpnaccounts.DevicePlatformWindows})
	if err != nil {
		t.Fatalf("create device B: %v", err)
	}
	deviceBHash := vpnaccounts.HashSubscriptionToken("device-b-raw-token")
	if _, err := accounts.CreateDeviceSubscriptionToken(ctx, deviceB.ID, deviceBHash, nil); err != nil {
		t.Fatalf("issue device B token: %v", err)
	}

	assertResolves := func(t *testing.T, hash string, wantResolves bool) {
		t.Helper()
		_, err := accounts.FindActiveSubscriptionTokenByHash(ctx, hash)
		switch {
		case wantResolves && err != nil:
			t.Fatalf("expected token to resolve, got error: %v", err)
		case !wantResolves && !errors.Is(err, pgx.ErrNoRows):
			t.Fatalf("expected token to no longer resolve, got err=%v", err)
		}
	}

	// 3. After legacy/account-level rotate, the pre-upgrade token no longer
	// resolves, and 4. the newly issued legacy token resolves.
	rotatedLegacy, err := accounts.CreateSubscriptionToken(ctx, vpnaccounts.CreateSubscriptionTokenInput{VPNAccountID: accountID, TokenHash: vpnaccounts.HashSubscriptionToken("post-rotate-legacy-token")})
	if err != nil {
		t.Fatalf("rotate legacy/account-level subscription: %v", err)
	}
	assertResolves(t, preUpgradeHash, false)
	assertResolves(t, rotatedLegacy.TokenHash, true)

	// 5. Unrelated Device A / Device B tokens remain active throughout.
	assertResolves(t, deviceAHash, true)
	assertResolves(t, deviceBHash, true)

	// 6. Portal rotation has the same isolation semantics: it revokes only
	// the current legacy token and leaves both devices untouched.
	portalToken, err := portalRepo.CreateSubscriptionToken(ctx, portal.CreateSubscriptionTokenInput{VPNAccountID: accountID, TokenHash: vpnaccounts.HashSubscriptionToken("portal-rotated-token")})
	if err != nil {
		t.Fatalf("portal rotate subscription: %v", err)
	}
	assertResolves(t, rotatedLegacy.TokenHash, false)
	assertResolves(t, portalToken.TokenHash, true)
	assertResolves(t, deviceAHash, true)
	assertResolves(t, deviceBHash, true)
}
