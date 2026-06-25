package traffic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDevTrafficUsageScenarioReportsCounterIncrease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routegate-dev-traffic-usage.json")
	collector := NewFileCollector(path)
	tracker := NewDeltaTracker()

	writeCounters := func(rxBytes int64, txBytes int64) {
		t.Helper()
		payload := fmt.Sprintf(`{
			"counters": [
				{
					"vpnAccountId": "account-1",
					"rxBytes": %d,
					"txBytes": %d,
					"metadata": {
						"source": "routegate_dev_traffic_usage",
						"scenario": "RG-73"
					}
				}
			]
		}`, rxBytes, txBytes)
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatalf("write dev traffic counters: %v", err)
		}
	}

	writeCounters(1_000_000, 2_000_000)
	baseline, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect baseline counters: %v", err)
	}
	if events := tracker.BuildUsageEvents(baseline); len(events) != 0 {
		t.Fatalf("expected first dev snapshot to establish baseline, got %d events", len(events))
	}

	writeCounters(1_500_000, 2_600_000)
	increased, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect increased counters: %v", err)
	}
	events := tracker.BuildUsageEvents(increased)
	if len(events) != 1 {
		t.Fatalf("expected one dev traffic delta event, got %d", len(events))
	}
	event := events[0]
	if event.VPNAccountID != "account-1" {
		t.Fatalf("unexpected vpn account id: %q", event.VPNAccountID)
	}
	if event.RxBytes != 500_000 || event.TxBytes != 600_000 {
		t.Fatalf("unexpected dev traffic delta: %+v", event)
	}
	if event.Metadata["source"] != "routegate_dev_traffic_usage" || event.Metadata["scenario"] != "RG-73" {
		t.Fatalf("unexpected dev traffic metadata: %+v", event.Metadata)
	}
}
