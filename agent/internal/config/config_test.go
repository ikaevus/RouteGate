package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	return path
}

func TestLoadTrafficCollectionDefaults(t *testing.T) {
	path := writeTestConfig(t, `manager_url: "http://127.0.0.1:8080"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.TrafficCollectionEnabled {
		t.Fatal("traffic collection should be disabled by default")
	}
	if cfg.TrafficCollectionIntervalSeconds != DefaultTrafficCollectionIntervalSeconds {
		t.Fatalf("expected default traffic interval %d, got %d", DefaultTrafficCollectionIntervalSeconds, cfg.TrafficCollectionIntervalSeconds)
	}
	if cfg.TrafficCollectionInterval() != time.Duration(DefaultTrafficCollectionIntervalSeconds)*time.Second {
		t.Fatalf("unexpected traffic collection interval: %s", cfg.TrafficCollectionInterval())
	}
	if cfg.TrafficUsageFilePath != DefaultTrafficUsageFilePath {
		t.Fatalf("expected default traffic file path %q, got %q", DefaultTrafficUsageFilePath, cfg.TrafficUsageFilePath)
	}
	if cfg.Hysteria2ActiveConfigPath != DefaultHysteria2ActiveConfigPath || cfg.Hysteria2Path != DefaultHysteria2Path || cfg.Hysteria2ServiceName != DefaultHysteria2ServiceName {
		t.Fatalf("unexpected Hysteria2 defaults: %+v", cfg)
	}
	if cfg.MTProtoActiveConfigPath != DefaultMTProtoActiveConfigPath || cfg.MTGPath != DefaultMTGPath || cfg.MTProtoServiceName != DefaultMTProtoServiceName {
		t.Fatalf("unexpected MTProto defaults: %+v", cfg)
	}
}

func TestLoadTrafficCollectionSettings(t *testing.T) {
	path := writeTestConfig(t, `manager_url: "http://127.0.0.1:8080"
traffic_collection_enabled: true
traffic_collection_interval_seconds: 120
traffic_usage_file_path: "/tmp/routegate-traffic.json"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !cfg.TrafficCollectionEnabled {
		t.Fatal("traffic collection should be enabled")
	}
	if cfg.TrafficCollectionIntervalSeconds != 120 {
		t.Fatalf("expected traffic interval 120, got %d", cfg.TrafficCollectionIntervalSeconds)
	}
	if cfg.TrafficUsageFilePath != "/tmp/routegate-traffic.json" {
		t.Fatalf("unexpected traffic file path: %q", cfg.TrafficUsageFilePath)
	}
}

func TestLoadRejectsInvalidTrafficCollectionEnabled(t *testing.T) {
	path := writeTestConfig(t, `manager_url: "http://127.0.0.1:8080"
traffic_collection_enabled: maybe
`)

	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid boolean to fail")
	}
}
