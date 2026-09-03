package presence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileCollectorMissingFileReturnsAuthoritativeEmptySnapshot(t *testing.T) {
	collector := NewFileCollector(filepath.Join(t.TempDir(), "missing.json"))
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	collector.now = func() time.Time { return now }

	snapshot, err := collector.Collect(context.Background())
	if err != nil { t.Fatal(err) }
	if !snapshot.ObservedAt.Equal(now) { t.Fatalf("observedAt=%s want %s", snapshot.ObservedAt, now) }
	if snapshot.Items == nil || len(snapshot.Items) != 0 { t.Fatalf("items=%v want an explicit empty slice", snapshot.Items) }
}

func TestFileCollectorNormalizesSafeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presence.json")
	data := []byte(`{"items":[{"vpnAccountId":" account-id ","protocol":" vless-reality ","connectionCount":2}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) }
	collector := NewFileCollector(path)
	collector.now = func() time.Time { return time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC) }

	snapshot, err := collector.Collect(context.Background())
	if err != nil { t.Fatal(err) }
	if len(snapshot.Items) != 1 { t.Fatalf("items=%d want 1", len(snapshot.Items)) }
	item := snapshot.Items[0]
	if item.VPNAccountID != "account-id" || item.Protocol != "vless-reality" { t.Fatalf("item=%+v", item) }
	if item.Source != FileCollectorSource || item.Confidence != "exact" { t.Fatalf("defaults=%+v", item) }
}

func TestFileCollectorRejectsInvalidConfidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presence.json")
	data := []byte(`{"items":[{"vpnAccountId":"account-id","protocol":"wireguard","connectionCount":1,"confidence":"certain"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) }
	_, err := NewFileCollector(path).Collect(context.Background())
	if err == nil { t.Fatal("expected invalid confidence error") }
}
