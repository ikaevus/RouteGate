package systeminfo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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
	operations, ok := capabilities["vpnCoreServiceOperations"].([]string)
	if !ok || len(operations) != 3 || operations[0] != "start" || operations[1] != "stop" || operations[2] != "restart" {
		t.Fatalf("unexpected VPN Core service operations capability: %#v", capabilities["vpnCoreServiceOperations"])
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
