package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

// TestVpnAccountDevicesBackfillNormalizesLegacyClientTypes verifies migration
// 000149's backfill maps every historical vpn_client_profiles.client_type
// value onto the RG-116 device allow-list (hiddify, v2rayn, v2rayng,
// generic), never preserving a retired identity (v2raytun, v2box) or any
// other historical/unknown value as a first-class device client. A
// backfilled device row that failed this would be rejected by the device
// API's own validation the next time it was renamed or rotated.
func TestVpnAccountDevicesBackfillNormalizesLegacyClientTypes(t *testing.T) {
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

	cases := []struct {
		legacyClientType string
		legacyDeviceType string
		wantClientType   string
		wantDeviceType   string
	}{
		{"hiddify", "ios", "hiddify", "ios"},
		{"v2rayn", "windows", "v2rayn", "windows"},
		{"v2rayng", "android", "v2rayng", "android"},
		{"v2raytun", "ios", "generic", "ios"},
		{"v2box", "android", "generic", "android"},
		{"other", "macos", "generic", "macos"},
		// sing-box is not being added to the RG-116 device allow-list by this
		// migration; that would be a silent product decision this pass must
		// not make. It normalizes to generic like any other unsupported value.
		{"sing-box", "linux", "generic", "linux"},
		{"some-unknown-legacy-value", "some-unknown-platform", "generic", "other"},
	}

	accountIDs := make([]string, len(cases))
	for i, tc := range cases {
		username := "rg116-backfill-normalize-" + tc.legacyClientType
		var accountID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO vpn_accounts (username, protocol, display_name, status)
			VALUES ($1, 'sing-box', $1, 'active')
			RETURNING id::text
		`, username).Scan(&accountID); err != nil {
			t.Fatalf("create account for %q: %v", tc.legacyClientType, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO vpn_client_profiles (vpn_account_id, client_type, device_type)
			VALUES ($1::uuid, $2, $3)
		`, accountID, tc.legacyClientType, tc.legacyDeviceType); err != nil {
			t.Fatalf("create legacy client profile for %q: %v", tc.legacyClientType, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
			VALUES ($1::uuid, $2, 'active')
		`, accountID, "backfill-normalize-hash-"+tc.legacyClientType); err != nil {
			t.Fatalf("create legacy subscription token for %q: %v", tc.legacyClientType, err)
		}
		accountIDs[i] = accountID
	}

	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply vpn_account_devices migration: %v", err)
	}

	allowedClientTypes := map[string]bool{"hiddify": true, "v2rayn": true, "v2rayng": true, "generic": true}
	allowedDeviceTypes := map[string]bool{"windows": true, "ios": true, "android": true, "macos": true, "linux": true, "other": true}

	for i, tc := range cases {
		var gotClientType, gotDeviceType string
		if err := pool.QueryRow(ctx, `
			SELECT client_type, device_type FROM vpn_account_devices WHERE vpn_account_id = $1::uuid
		`, accountIDs[i]).Scan(&gotClientType, &gotDeviceType); err != nil {
			t.Fatalf("read backfilled device for legacy client_type %q: %v", tc.legacyClientType, err)
		}
		if gotClientType != tc.wantClientType {
			t.Fatalf("legacy client_type %q backfilled to %q, want %q", tc.legacyClientType, gotClientType, tc.wantClientType)
		}
		if gotDeviceType != tc.wantDeviceType {
			t.Fatalf("legacy device_type %q (client_type %q) backfilled to %q, want %q", tc.legacyDeviceType, tc.legacyClientType, gotDeviceType, tc.wantDeviceType)
		}
		if !allowedClientTypes[gotClientType] {
			t.Fatalf("backfilled client_type %q is not in the RG-116 device allow-list", gotClientType)
		}
		if !allowedDeviceTypes[gotDeviceType] {
			t.Fatalf("backfilled device_type %q is not in the RG-116 device allow-list", gotDeviceType)
		}
	}
}

// TestVpnAccountDevicesLegacyNullDeviceTokenInvariant verifies migration
// 000149 enforces, at the database level, both halves of the post-RG-116
// uniqueness contract: one active token per device, AND one active
// device_id-IS-NULL ("legacy") token per account for the pre-existing
// account-level subscription-token endpoints, which remain supported. A
// legacy token and one or more device tokens may coexist on the same
// account; that is an intentional backward-compatibility allowance, not an
// accident.
func TestVpnAccountDevicesLegacyNullDeviceTokenInvariant(t *testing.T) {
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
		t.Fatalf("apply current migrations: %v", err)
	}

	var accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_accounts (username, protocol, display_name, status)
		VALUES ('rg116-legacy-invariant-fixture', 'sing-box', 'RG-116 legacy invariant fixture', 'active')
		RETURNING id::text
	`).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// One legacy (device_id IS NULL) active token, issued the same way the
	// pre-existing account-level subscription-token endpoints do.
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
		VALUES ($1::uuid, 'legacy-invariant-hash-1', 'active')
	`, accountID); err != nil {
		t.Fatalf("create first legacy token: %v", err)
	}

	// A second legacy active token on the SAME account must be rejected: at
	// most one device_id-IS-NULL active token per account.
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
		VALUES ($1::uuid, 'legacy-invariant-hash-2', 'active')
	`, accountID); err == nil {
		t.Fatal("expected a second active legacy (device_id IS NULL) token on the same account to violate the DB-level invariant")
	}

	// A device-scoped active token on the SAME account must coexist fine
	// alongside the still-active legacy token: legacy and per-device
	// uniqueness are independent invariants by design.
	var deviceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO vpn_account_devices (vpn_account_id, name, client_type, device_type)
		VALUES ($1::uuid, 'Coexisting device', 'hiddify', 'ios')
		RETURNING id::text
	`, accountID).Scan(&deviceID); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, device_id, token_hash, status)
		VALUES ($1::uuid, $2::uuid, 'legacy-invariant-device-hash', 'active')
	`, accountID, deviceID); err != nil {
		t.Fatalf("expected a device-scoped token to coexist with the account's active legacy token: %v", err)
	}

	var activeTokenCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vpn_subscription_tokens WHERE vpn_account_id = $1::uuid AND status = 'active'
	`, accountID).Scan(&activeTokenCount); err != nil {
		t.Fatalf("count active tokens: %v", err)
	}
	if activeTokenCount != 2 {
		t.Fatalf("active token count = %d, want 2 (one legacy + one device-scoped coexisting)", activeTokenCount)
	}
}

// TestVpnAccountDevicesDownMigrationAbortsWhenUnrepresentable verifies the
// 000149 down migration is fail-safe: it must abort atomically, before any
// destructive change, when an account currently has more active tokens than
// the pre-RG-116 schema (one active token per account, full stop) could
// represent - rather than silently revoking a valid device/user credential
// to force the rollback through.
func TestVpnAccountDevicesDownMigrationAbortsWhenUnrepresentable(t *testing.T) {
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

	downSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000149_vpn_account_devices.down.sql"))
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	// 000150 adds deliveries.device_id, a foreign key into vpn_account_devices.
	// Rolling back 000149 alone (without first rolling back what was layered
	// on top of it) would fail on that dependency regardless of the
	// token-count preflight this test exercises, so roll back in the correct
	// reverse order for the "succeeds" case below.
	downSQL150, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000150_delivery_device_scope.down.sql"))
	if err != nil {
		t.Fatalf("read 000150 down migration: %v", err)
	}
	downSQL151, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000151_delivery_token_generation.down.sql"))
	if err != nil {
		t.Fatalf("read 000151 down migration: %v", err)
	}

	t.Run("aborts and changes nothing when unrepresentable", func(t *testing.T) {
		resetPublicSchema(t, ctx, pool)
		if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
			t.Fatalf("apply current migrations: %v", err)
		}

		var accountID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO vpn_accounts (username, protocol, display_name, status)
			VALUES ('rg116-down-unsafe-fixture', 'sing-box', 'RG-116 down unsafe fixture', 'active')
			RETURNING id::text
		`).Scan(&accountID); err != nil {
			t.Fatalf("create account: %v", err)
		}
		for _, name := range []string{"Device A", "Device B"} {
			var deviceID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO vpn_account_devices (vpn_account_id, name, client_type, device_type)
				VALUES ($1::uuid, $2, 'hiddify', 'ios')
				RETURNING id::text
			`, accountID, name).Scan(&deviceID); err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO vpn_subscription_tokens (vpn_account_id, device_id, token_hash, status)
				VALUES ($1::uuid, $2::uuid, $3, 'active')
			`, accountID, deviceID, "down-unsafe-hash-"+name); err != nil {
				t.Fatalf("issue token for %s: %v", name, err)
			}
		}

		if _, err := pool.Exec(ctx, string(downSQL)); err == nil {
			t.Fatal("expected the down migration to abort when an account has more active tokens than the old schema can represent")
		}

		var deviceCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM vpn_account_devices WHERE vpn_account_id = $1::uuid`, accountID).Scan(&deviceCount); err != nil {
			t.Fatalf("count devices after aborted rollback: %v", err)
		}
		if deviceCount != 2 {
			t.Fatalf("device count after aborted rollback = %d, want 2 (rollback must not have partially applied)", deviceCount)
		}
		var activeTokenCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM vpn_subscription_tokens WHERE vpn_account_id = $1::uuid AND status = 'active'`, accountID).Scan(&activeTokenCount); err != nil {
			t.Fatalf("count active tokens after aborted rollback: %v", err)
		}
		if activeTokenCount != 2 {
			t.Fatalf("active token count after aborted rollback = %d, want 2 (no credential must have been silently revoked)", activeTokenCount)
		}
	})

	t.Run("succeeds when every account has at most one active token", func(t *testing.T) {
		resetPublicSchema(t, ctx, pool)
		if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
			t.Fatalf("apply current migrations: %v", err)
		}

		var accountID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO vpn_accounts (username, protocol, display_name, status)
			VALUES ('rg116-down-safe-fixture', 'sing-box', 'RG-116 down safe fixture', 'active')
			RETURNING id::text
		`).Scan(&accountID); err != nil {
			t.Fatalf("create account: %v", err)
		}
		var deviceID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO vpn_account_devices (vpn_account_id, name, client_type, device_type)
			VALUES ($1::uuid, 'Only device', 'hiddify', 'ios')
			RETURNING id::text
		`, accountID).Scan(&deviceID); err != nil {
			t.Fatalf("create device: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO vpn_subscription_tokens (vpn_account_id, device_id, token_hash, status)
			VALUES ($1::uuid, $2::uuid, 'down-safe-hash', 'active')
		`, accountID, deviceID); err != nil {
			t.Fatalf("issue token: %v", err)
		}

		if _, err := pool.Exec(ctx, string(downSQL151)); err != nil {
			t.Fatalf("roll back 000151 first (reverse order): %v", err)
		}
		if _, err := pool.Exec(ctx, string(downSQL150)); err != nil {
			t.Fatalf("roll back 000150 next (reverse order): %v", err)
		}
		if _, err := pool.Exec(ctx, string(downSQL)); err != nil {
			t.Fatalf("expected the down migration to succeed when every account has at most one active token: %v", err)
		}

		var subscriptionTokenIDColumnExists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'deliveries' AND column_name = 'subscription_token_id'
			)
		`).Scan(&subscriptionTokenIDColumnExists); err != nil {
			t.Fatalf("check deliveries.subscription_token_id column: %v", err)
		}
		if subscriptionTokenIDColumnExists {
			t.Fatal("expected deliveries.subscription_token_id to be dropped after rolling back 000151")
		}

		var deviceTableExists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'vpn_account_devices')`).Scan(&deviceTableExists); err != nil {
			t.Fatalf("check vpn_account_devices table: %v", err)
		}
		if deviceTableExists {
			t.Fatal("expected vpn_account_devices to be dropped after a successful rollback")
		}
		var deviceIDColumnExists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'vpn_subscription_tokens' AND column_name = 'device_id'
			)
		`).Scan(&deviceIDColumnExists); err != nil {
			t.Fatalf("check vpn_subscription_tokens.device_id column: %v", err)
		}
		if deviceIDColumnExists {
			t.Fatal("expected vpn_subscription_tokens.device_id to be dropped after a successful rollback")
		}
	})
}
