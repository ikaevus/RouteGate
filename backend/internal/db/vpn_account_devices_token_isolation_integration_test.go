package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/portal"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// TestLegacyAndDeviceSubscriptionTokensAreIsolated is the regression test for
// the RG-115/RG-116 integration bug where account-level ("legacy") token
// operations revoked ALL active tokens for the account - including per-device
// RG-116 tokens - because their SQL only filtered on vpn_account_id and
// status, not device_id. It exercises the real vpnaccounts.Repository and
// portal.Repository methods (not raw SQL) against a real database, so it
// pins the actual code path administrators and Portal self-service users hit.
func TestLegacyAndDeviceSubscriptionTokensAreIsolated(t *testing.T) {
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

	accounts := vpnaccounts.NewRepository(pool)
	portalRepo := portal.NewRepository(pool)

	account, err := accounts.CreateAccount(ctx, vpnaccounts.CreateAccountInput{DisplayName: "rg116-token-isolation-fixture", Status: vpnaccounts.StatusActive})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	legacyToken, err := accounts.CreateSubscriptionToken(ctx, vpnaccounts.CreateSubscriptionTokenInput{VPNAccountID: account.ID, TokenHash: "isolation-legacy-hash-1"})
	if err != nil {
		t.Fatalf("issue legacy token: %v", err)
	}

	deviceA, err := accounts.CreateDevice(ctx, vpnaccounts.CreateDeviceInput{VPNAccountID: account.ID, Name: "Device A", ClientType: vpnaccounts.ClientTypeHiddify, DeviceType: vpnaccounts.DevicePlatformIOS})
	if err != nil {
		t.Fatalf("create device A: %v", err)
	}
	deviceAToken, err := accounts.CreateDeviceSubscriptionToken(ctx, deviceA.ID, "isolation-device-a-hash-1", nil)
	if err != nil {
		t.Fatalf("issue device A token: %v", err)
	}

	deviceB, err := accounts.CreateDevice(ctx, vpnaccounts.CreateDeviceInput{VPNAccountID: account.ID, Name: "Device B", ClientType: vpnaccounts.ClientTypeV2RayN, DeviceType: vpnaccounts.DevicePlatformWindows})
	if err != nil {
		t.Fatalf("create device B: %v", err)
	}
	deviceBToken, err := accounts.CreateDeviceSubscriptionToken(ctx, deviceB.ID, "isolation-device-b-hash-1", nil)
	if err != nil {
		t.Fatalf("issue device B token: %v", err)
	}

	assertTokenStatus := func(t *testing.T, tokenID, want string) {
		t.Helper()
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM vpn_subscription_tokens WHERE id = $1::uuid`, tokenID).Scan(&status); err != nil {
			t.Fatalf("read token %s status: %v", tokenID, err)
		}
		if status != want {
			t.Fatalf("token %s status = %q, want %q", tokenID, status, want)
		}
	}

	// 1. All three coexist.
	assertTokenStatus(t, legacyToken.ID, "active")
	assertTokenStatus(t, deviceAToken.ID, "active")
	assertTokenStatus(t, deviceBToken.ID, "active")

	// 2. Rotating the legacy token must only invalidate the previous legacy
	// token, never a device token.
	rotatedLegacyToken, err := accounts.CreateSubscriptionToken(ctx, vpnaccounts.CreateSubscriptionTokenInput{VPNAccountID: account.ID, TokenHash: "isolation-legacy-hash-2"})
	if err != nil {
		t.Fatalf("rotate legacy token: %v", err)
	}
	assertTokenStatus(t, legacyToken.ID, "revoked")
	assertTokenStatus(t, rotatedLegacyToken.ID, "active")
	assertTokenStatus(t, deviceAToken.ID, "active")
	assertTokenStatus(t, deviceBToken.ID, "active")

	// 3. Revoking the legacy token must leave both device tokens active.
	if err := accounts.RevokeActiveSubscriptionTokens(ctx, account.ID); err != nil {
		t.Fatalf("revoke legacy token: %v", err)
	}
	assertTokenStatus(t, rotatedLegacyToken.ID, "revoked")
	assertTokenStatus(t, deviceAToken.ID, "active")
	assertTokenStatus(t, deviceBToken.ID, "active")

	// Re-issue a legacy token so the next assertions can prove rotating
	// Device A leaves it (and Device B) alone.
	legacyToken2, err := accounts.CreateSubscriptionToken(ctx, vpnaccounts.CreateSubscriptionTokenInput{VPNAccountID: account.ID, TokenHash: "isolation-legacy-hash-3"})
	if err != nil {
		t.Fatalf("re-issue legacy token: %v", err)
	}

	// 4. Rotating Device A must leave the legacy token and Device B alone.
	rotatedDeviceAToken, err := accounts.CreateDeviceSubscriptionToken(ctx, deviceA.ID, "isolation-device-a-hash-2", nil)
	if err != nil {
		t.Fatalf("rotate device A token: %v", err)
	}
	assertTokenStatus(t, deviceAToken.ID, "revoked")
	assertTokenStatus(t, rotatedDeviceAToken.ID, "active")
	assertTokenStatus(t, legacyToken2.ID, "active")
	assertTokenStatus(t, deviceBToken.ID, "active")

	// 5. Portal token generation/rotation must leave RG-116 device tokens
	// (and use the same device_id-IS-NULL legacy scope) untouched.
	portalToken, err := portalRepo.CreateSubscriptionToken(ctx, portal.CreateSubscriptionTokenInput{VPNAccountID: account.ID, TokenHash: "isolation-portal-hash-1"})
	if err != nil {
		t.Fatalf("portal create subscription token: %v", err)
	}
	assertTokenStatus(t, legacyToken2.ID, "revoked")
	assertTokenStatus(t, portalToken.ID, "active")
	assertTokenStatus(t, rotatedDeviceAToken.ID, "active")
	assertTokenStatus(t, deviceBToken.ID, "active")

	var portalTokenDeviceID string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(device_id::text, '') FROM vpn_subscription_tokens WHERE id = $1::uuid`, portalToken.ID).Scan(&portalTokenDeviceID); err != nil {
		t.Fatalf("read portal token device_id: %v", err)
	}
	if portalTokenDeviceID != "" {
		t.Fatalf("portal token device_id = %q, want empty (legacy/device_id IS NULL)", portalTokenDeviceID)
	}
}
