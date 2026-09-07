package connections

import (
	"testing"
	"time"
)

func TestPresenceItemExpiryDoesNotRefreshOldHeuristicActivity(t *testing.T) {
	observedAt := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	lastActivityAt := observedAt.Add(-30 * time.Second)
	got := presenceItemExpiry(observedAt, SnapshotItem{Confidence: "heuristic", LastActivityAt: &lastActivityAt})
	want := lastActivityAt.Add(PresenceTTL)
	if !got.Equal(want) { t.Fatalf("expiry=%s want %s", got, want) }
}

func TestPresenceItemExpiryKeepsExactSnapshotFresh(t *testing.T) {
	observedAt := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	lastActivityAt := observedAt.Add(-time.Hour)
	got := presenceItemExpiry(observedAt, SnapshotItem{Confidence: "exact", LastActivityAt: &lastActivityAt})
	want := observedAt.Add(PresenceTTL)
	if !got.Equal(want) { t.Fatalf("expiry=%s want %s", got, want) }
}
