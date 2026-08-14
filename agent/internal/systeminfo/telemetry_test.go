package systeminfo

import "testing"

func TestParseMemoryInfo(t *testing.T) {
	total, available, ok := parseMemoryInfo([]byte("MemTotal:       8192000 kB\nMemFree:         512000 kB\nMemAvailable:   4096000 kB\n"))
	if !ok {
		t.Fatal("expected memory info to parse")
	}
	if total != 8192000*1024 || available != 4096000*1024 {
		t.Fatalf("unexpected memory bytes: total=%d available=%d", total, available)
	}
}

func TestParseMemoryInfoRejectsImpossibleBounds(t *testing.T) {
	if _, _, ok := parseMemoryInfo([]byte("MemTotal: 100 kB\nMemAvailable: 200 kB\n")); ok {
		t.Fatal("memory info with available above total must be rejected")
	}
}

func TestParseUptime(t *testing.T) {
	uptime, ok := parseUptime([]byte("12345.67 9876.54\n"))
	if !ok {
		t.Fatal("expected uptime to parse")
	}
	if uptime != 12345 {
		t.Fatalf("uptime = %d, want 12345", uptime)
	}
}

func TestTelemetryVPNCoreNotInstalled(t *testing.T) {
	core := telemetryVPNCore(map[string]any{
		"type":      "sing-box",
		"installed": false,
	})
	if core.Type != "sing-box" || core.Installed || core.ServiceState != "not_installed" {
		t.Fatalf("unexpected VPN Core telemetry: %+v", core)
	}
}

func TestTelemetryVPNCoreRunning(t *testing.T) {
	core := telemetryVPNCore(map[string]any{
		"type":         "sing-box",
		"installed":    true,
		"version":      "sing-box version 1.12.0",
		"serviceState": "active",
	})
	if !core.Installed || core.Version != "sing-box version 1.12.0" || core.ServiceState != "active" {
		t.Fatalf("unexpected VPN Core telemetry: %+v", core)
	}
}
