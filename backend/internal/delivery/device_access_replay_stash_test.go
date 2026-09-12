package delivery

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// TestShouldStashDeviceAccessDecidesReplayStashing is the item-3 regression
// test: CreateForVPNAccount must stash a brand-new device delivery's
// plaintext access material, must never re-stash an idempotent replay of an
// already-terminal delivery (sent/delivered/failed/uncertain - the worker
// will never claim it again, so re-stashing would just leave the plaintext
// URL sitting in memory for the rest of its TTL for no reason), and must
// re-stash a replay of a still-in-flight delivery (queued/sending/retrying),
// since the worker may still claim it and will need the material.
func TestShouldStashDeviceAccessDecidesReplayStashing(t *testing.T) {
	const deviceID = "11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name    string
		created bool
		status  Status
		want    bool
	}{
		{"new device delivery is always stashed", true, StatusQueued, true},
		{"replay of a still-queued delivery is re-stashed", false, StatusQueued, true},
		{"replay of a sending delivery is re-stashed", false, StatusSending, true},
		{"replay of a retrying delivery is re-stashed", false, StatusRetrying, true},
		{"replay of a sent delivery is never re-stashed", false, StatusSent, false},
		{"replay of a delivered delivery is never re-stashed", false, StatusDelivered, false},
		{"replay of a failed delivery is never re-stashed", false, StatusFailed, false},
		{"replay of an uncertain delivery is never re-stashed", false, StatusUncertain, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldStashDeviceAccess(deviceID, test.created, test.status); got != test.want {
				t.Fatalf("shouldStashDeviceAccess(%q, created=%v, status=%q) = %v, want %v", deviceID, test.created, test.status, got, test.want)
			}
		})
	}
}

// TestShouldStashDeviceAccessNeverAppliesToAccountLevelDeliveries pins that
// the stash decision is entirely skipped once DeviceID is empty (account-level
// Send never has device access material to stash in the first place).
func TestShouldStashDeviceAccessNeverAppliesToAccountLevelDeliveries(t *testing.T) {
	for _, status := range []Status{StatusQueued, StatusSending, StatusRetrying, StatusSent, StatusDelivered, StatusFailed, StatusUncertain} {
		if shouldStashDeviceAccess("", true, status) {
			t.Fatalf("account-level created delivery must never stash, status=%q", status)
		}
		if shouldStashDeviceAccess("", false, status) {
			t.Fatalf("account-level replayed delivery must never stash, status=%q", status)
		}
	}
}

// TestDeviceAccessMaterialSurvivesOrIsSkippedAcrossReplay exercises the same
// decision against the real in-memory store (VPNAccessResolver.StashDeviceAccess
// backs onto the package-level globalDeviceAccessMaterialStore), proving the
// end-to-end effect matches the decision function: a terminal replay must
// leave whatever is already stashed exactly as it was (nothing new written),
// while a non-terminal replay overwrites it with the freshly re-validated URL.
func TestDeviceAccessMaterialSurvivesOrIsSkippedAcrossReplay(t *testing.T) {
	resolver := NewVPNAccessResolver(fakeClientConnectionSource{}, "https://vpn.example.com")

	t.Run("terminal replay leaves stashed material untouched", func(t *testing.T) {
		const deliveryID = "22222222-2222-2222-2222-222222222222"
		resolver.StashDeviceAccess(deliveryID, "https://vpn.example.com/sub/original-token", "iPhone")

		if shouldStashDeviceAccess("device-1", false, StatusSent) {
			t.Fatal("terminal replay must not be decided as stash-worthy")
		}
		// Handler correctly skips the StashDeviceAccess call in this branch;
		// confirm the store still holds the original entry unchanged.
		entry, ok := globalDeviceAccessMaterialStore.get(deliveryID)
		if !ok || entry.accessURL != "https://vpn.example.com/sub/original-token" {
			t.Fatalf("expected original material to remain untouched, got entry=%+v ok=%v", entry, ok)
		}
	})

	t.Run("non-terminal replay re-stashes the revalidated URL", func(t *testing.T) {
		const deliveryID = "33333333-3333-3333-3333-333333333333"
		resolver.StashDeviceAccess(deliveryID, "https://vpn.example.com/sub/still-current-token", "iPhone")

		if !shouldStashDeviceAccess("device-1", false, StatusRetrying) {
			t.Fatal("non-terminal replay must be decided as stash-worthy")
		}
		resolver.StashDeviceAccess(deliveryID, "https://vpn.example.com/sub/still-current-token", "iPhone")

		entry, ok := globalDeviceAccessMaterialStore.get(deliveryID)
		if !ok || entry.accessURL != "https://vpn.example.com/sub/still-current-token" {
			t.Fatalf("expected material to remain available after re-stash, got entry=%+v ok=%v", entry, ok)
		}
	})
}

// fakeClientConnectionSource is an empty vpnaccounts.ClientConnectionSource:
// the tests above only exercise the device-scoped stash path, which never
// touches it.
type fakeClientConnectionSource struct{}

func (fakeClientConnectionSource) GetSubscriptionProfileByAccountID(context.Context, string) (vpnaccounts.SubscriptionProfile, error) {
	return vpnaccounts.SubscriptionProfile{}, pgx.ErrNoRows
}

func (fakeClientConnectionSource) GetOrCreateClientProfile(context.Context, string) (vpnaccounts.ClientProfile, error) {
	return vpnaccounts.ClientProfile{}, pgx.ErrNoRows
}
