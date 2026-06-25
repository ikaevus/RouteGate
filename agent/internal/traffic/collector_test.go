package traffic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileCollectorReadsCounterSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	if err := os.WriteFile(path, []byte(`{
		"counters": [
			{"vpnAccountId":" account-1 ","rxBytes":1000,"txBytes":2000,"observedAt":"2026-06-25T10:00:00Z","metadata":{"runtime":"test"}}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write traffic file: %v", err)
	}
	collector := NewFileCollector(path)

	snapshots, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect traffic: %v", err)
	}

	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.VPNAccountID != "account-1" {
		t.Fatalf("unexpected account id: %q", snapshot.VPNAccountID)
	}
	if snapshot.RxBytes != 1000 || snapshot.TxBytes != 2000 {
		t.Fatalf("unexpected counters: %+v", snapshot)
	}
	if snapshot.ObservedAt != time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected observedAt: %s", snapshot.ObservedAt)
	}
	if snapshot.Metadata["source"] != FileCollectorSource {
		t.Fatalf("expected collector source metadata, got %+v", snapshot.Metadata)
	}
	if snapshot.Metadata["runtime"] != "test" {
		t.Fatalf("expected runtime metadata to be preserved, got %+v", snapshot.Metadata)
	}
}

func TestFileCollectorMissingFileIsEmpty(t *testing.T) {
	collector := NewFileCollector(filepath.Join(t.TempDir(), "missing.json"))

	snapshots, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect traffic: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("expected no snapshots, got %d", len(snapshots))
	}
}

func TestFileCollectorRejectsNegativeCounters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	if err := os.WriteFile(path, []byte(`[{"vpnAccountId":"account-1","rxBytes":-1,"txBytes":0}]`), 0o600); err != nil {
		t.Fatalf("write traffic file: %v", err)
	}
	collector := NewFileCollector(path)

	if _, err := collector.Collect(context.Background()); err == nil {
		t.Fatal("expected negative counters to fail")
	}
}

func TestDeltaTrackerSkipsFirstSnapshotAndReportsDeltas(t *testing.T) {
	tracker := NewDeltaTracker()
	observedAt := time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC)

	first := tracker.BuildUsageEvents([]CounterSnapshot{{VPNAccountID: "account-1", RxBytes: 100, TxBytes: 200, ObservedAt: observedAt}})
	if len(first) != 0 {
		t.Fatalf("expected first snapshot to establish baseline, got %d events", len(first))
	}

	second := tracker.BuildUsageEvents([]CounterSnapshot{{VPNAccountID: "account-1", RxBytes: 180, TxBytes: 260, ObservedAt: observedAt.Add(time.Minute)}})
	if len(second) != 1 {
		t.Fatalf("expected one delta event, got %d", len(second))
	}
	if second[0].RxBytes != 80 || second[0].TxBytes != 60 {
		t.Fatalf("unexpected delta event: %+v", second[0])
	}
}

func TestDeltaTrackerSkipsCounterReset(t *testing.T) {
	tracker := NewDeltaTracker()
	observedAt := time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC)

	tracker.BuildUsageEvents([]CounterSnapshot{{VPNAccountID: "account-1", RxBytes: 1000, TxBytes: 2000, ObservedAt: observedAt}})
	reset := tracker.BuildUsageEvents([]CounterSnapshot{{VPNAccountID: "account-1", RxBytes: 10, TxBytes: 20, ObservedAt: observedAt.Add(time.Minute)}})
	if len(reset) != 0 {
		t.Fatalf("expected reset snapshot to be skipped, got %d events", len(reset))
	}

	next := tracker.BuildUsageEvents([]CounterSnapshot{{VPNAccountID: "account-1", RxBytes: 25, TxBytes: 40, ObservedAt: observedAt.Add(2 * time.Minute)}})
	if len(next) != 1 {
		t.Fatalf("expected post-reset delta event, got %d", len(next))
	}
	if next[0].RxBytes != 15 || next[0].TxBytes != 20 {
		t.Fatalf("unexpected post-reset delta: %+v", next[0])
	}
}
