package tasks

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCheckVLESSListenerSucceedsForReachablePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	configPath := writeListenerTestConfig(t, []map[string]any{{
		"type":        "vless",
		"listen":      "127.0.0.1",
		"listen_port": port,
	}})

	result, err := CheckVLESSListener(context.Background(), configPath)
	if err != nil {
		t.Fatalf("listener healthcheck: %v", err)
	}
	if result.Port != port {
		t.Fatalf("port = %d, want %d", result.Port, port)
	}
	if result.Address != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		t.Fatalf("address = %q", result.Address)
	}
}

func TestCheckVLESSListenerRejectsMissingVLESSInbound(t *testing.T) {
	configPath := writeListenerTestConfig(t, []map[string]any{{
		"type":        "shadowsocks",
		"listen_port": 1080,
	}})

	if _, err := CheckVLESSListener(context.Background(), configPath); err == nil {
		t.Fatal("expected missing VLESS listener error")
	}
}

func TestCheckVLESSListenerRejectsUnreachablePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	configPath := writeListenerTestConfig(t, []map[string]any{{
		"type":        "vless",
		"listen_port": port,
	}})
	result, err := CheckVLESSListener(context.Background(), configPath)
	if err == nil {
		t.Fatal("expected unreachable listener error")
	}
	if result.Port != port {
		t.Fatalf("port = %d, want %d", result.Port, port)
	}
}

func writeListenerTestConfig(t *testing.T, inbounds []map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"inbounds": inbounds})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
