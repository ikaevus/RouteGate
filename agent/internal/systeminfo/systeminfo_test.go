package systeminfo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/vpncoreinstall"
)

func TestParseLoadAverage(t *testing.T) {
	load1, load5, load15, ok := parseLoadAverage([]byte("0.42 0.25 0.17 1/281 12345\n"))
	if !ok {
		t.Fatal("expected load average to parse")
	}
	if load1 != 0.42 || load5 != 0.25 || load15 != 0.17 {
		t.Fatalf("unexpected load average: %v %v %v", load1, load5, load15)
	}
}

func TestParseLoadAverageRejectsInvalidPayload(t *testing.T) {
	testCases := [][]byte{
		[]byte("0.42 0.25"),
		[]byte("invalid 0.25 0.17"),
		[]byte("-1 0.25 0.17"),
	}
	for _, payload := range testCases {
		if _, _, _, ok := parseLoadAverage(payload); ok {
			t.Fatalf("expected invalid payload to be rejected: %q", payload)
		}
	}
}

func TestDetectVPNCoreNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	status := detectVPNCore()

	if installed, _ := status["installed"].(bool); installed {
		t.Fatalf("expected VPN core to be absent, got %#v", status)
	}
	if got := status["state"]; got != "not_installed" {
		t.Fatalf("expected not_installed state, got %v", got)
	}
}

func TestDetectVPNCoreRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses executable shell scripts")
	}

	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "sing-box"), "#!/bin/sh\necho 'sing-box version 1.12.0'\n")
	writeExecutable(t, filepath.Join(binDir, "systemctl"), "#!/bin/sh\necho active\n")
	t.Setenv("PATH", binDir)

	status := detectVPNCore()

	if installed, _ := status["installed"].(bool); !installed {
		t.Fatalf("expected VPN core to be installed, got %#v", status)
	}
	if got := status["state"]; got != "running" {
		t.Fatalf("expected running state, got %v", got)
	}
	if got := status["version"]; got != "sing-box version 1.12.0" {
		t.Fatalf("unexpected version: %v", got)
	}
	if got := status["serviceState"]; got != "active" {
		t.Fatalf("unexpected service state: %v", got)
	}
}

func TestDetectVPNCoreStopped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses executable shell scripts")
	}

	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "sing-box"), "#!/bin/sh\necho 'sing-box version 1.12.0'\n")
	writeExecutable(t, filepath.Join(binDir, "systemctl"), "#!/bin/sh\necho inactive\nexit 3\n")
	t.Setenv("PATH", binDir)

	status := detectVPNCore()

	if got := status["state"]; got != "stopped" {
		t.Fatalf("expected stopped state, got %v", got)
	}
}

func TestDetectCapabilitiesIncludesVPNCore(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	capabilities := DetectCapabilities()
	vpnCore, ok := capabilities["vpnCore"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured vpnCore capability, got %#v", capabilities["vpnCore"])
	}
	if got := vpnCore["type"]; got != "sing-box" {
		t.Fatalf("expected sing-box core type, got %v", got)
	}
	routeGate, ok := capabilities["routegate"].(map[string]any)
	if !ok {
		t.Fatalf("expected versioned RouteGate platform capability, got %#v", capabilities["routegate"])
	}
	if got := routeGate["schemaVersion"]; got != platformCapabilitySchemaVersion {
		t.Fatalf("platform capability schemaVersion = %#v, want %d", got, platformCapabilitySchemaVersion)
	}
	adapters, ok := routeGate["vpnCoreAdapters"].([]map[string]any)
	if !ok || len(adapters) != 3 {
		t.Fatalf("unexpected managed VPN Core adapters: %#v", routeGate["vpnCoreAdapters"])
	}
	if adapters[0]["core"] != "sing-box" || adapters[0]["protocol"] != "vless" {
		t.Fatalf("unexpected managed adapter: %#v", adapters[0])
	}
	if adapters[1]["core"] != "wireguard" || adapters[1]["protocol"] != "wireguard" {
		t.Fatalf("unexpected WireGuard managed adapter: %#v", adapters[1])
	}
	if adapters[2]["core"] != "hysteria" || adapters[2]["protocol"] != "hysteria2" {
		t.Fatalf("unexpected Hysteria2 managed adapter: %#v", adapters[2])
	}
	operations, ok := capabilities["vpnCoreServiceOperations"].([]string)
	if !ok || len(operations) != 3 || operations[0] != "start" || operations[1] != "stop" || operations[2] != "restart" {
		t.Fatalf("unexpected VPN Core service operations capability: %#v", capabilities["vpnCoreServiceOperations"])
	}
	installationOperations, advertised := capabilities["vpnCoreInstallationOperations"]
	if vpncoreinstall.SupportsCurrentPlatform() {
		operations, ok := installationOperations.([]string)
		if !advertised || !ok || len(operations) != 1 || operations[0] != vpncoreinstall.OperationInstall {
			t.Fatalf("unexpected installation capability: %#v", installationOperations)
		}
	} else if advertised {
		t.Fatalf("installation capability advertised on unsupported environment: %#v", installationOperations)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
