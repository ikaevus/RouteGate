package systeminfo

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/buildinfo"
	"github.com/ikaevus/routegate/agent/internal/vpncoreinstall"
)

const vpnCoreCheckTimeout = 3 * time.Second

type Info struct {
	Hostname        string         `json:"hostname"`
	AgentVersion    string         `json:"agentVersion"`
	ProtocolVersion int            `json:"protocolVersion"`
	OS              string         `json:"os"`
	Arch            string         `json:"arch"`
	Capabilities    map[string]any `json:"capabilities"`
}

func Collect() Info {
	hostname, _ := os.Hostname()
	info := buildinfo.Current()
	return Info{Hostname: hostname, AgentVersion: info.Version, ProtocolVersion: info.ProtocolVersion, OS: runtime.GOOS, Arch: runtime.GOARCH, Capabilities: DetectCapabilities()}
}

func DetectCapabilities() map[string]any {
	names := []string{"systemctl", "sing-box", "xray", "nft"}
	caps := make(map[string]any, len(names)+2)
	for _, name := range names {
		_, err := exec.LookPath(name)
		caps[name] = err == nil
	}
	caps["vpnCore"] = detectVPNCore()
	caps["vpnCoreServiceOperations"] = []string{"start", "stop", "restart"}
	if vpncoreinstall.SupportsCurrentPlatform() {
		caps["vpnCoreInstallationOperations"] = []string{vpncoreinstall.OperationInstall}
	}
	return caps
}

func detectVPNCore() map[string]any {
	status := map[string]any{
		"type":      "sing-box",
		"installed": false,
		"state":     "not_installed",
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
	}

	binaryPath, err := exec.LookPath("sing-box")
	if err != nil {
		return status
	}

	status["installed"] = true
	status["binaryPath"] = binaryPath
	status["state"] = "installed"

	if output, commandErr, timedOut := runCommand(binaryPath, "version"); commandErr == nil {
		if version := firstNonEmptyLine(string(output)); version != "" {
			status["version"] = version
		}
	} else if timedOut {
		status["versionError"] = "version_check_timeout"
	} else {
		status["versionError"] = "version_check_failed"
	}

	if _, systemctlErr := exec.LookPath("systemctl"); systemctlErr != nil {
		status["serviceState"] = "unknown"
		return status
	}

	serviceOutput, serviceErr, timedOut := runCommand("systemctl", "is-active", "sing-box")
	serviceState := strings.TrimSpace(string(serviceOutput))
	if timedOut {
		serviceState = "unknown"
		status["serviceError"] = "service_check_timeout"
	} else if serviceState == "" {
		serviceState = "unknown"
	}
	status["serviceName"] = "sing-box.service"
	status["serviceState"] = serviceState

	switch serviceState {
	case "active":
		status["state"] = "running"
	case "inactive", "deactivating":
		status["state"] = "stopped"
	case "failed":
		status["state"] = "failed"
	case "activating", "reloading":
		status["state"] = "installed"
	default:
		if serviceErr != nil {
			status["state"] = "unknown"
		}
	}

	return status
}

func runCommand(name string, args ...string) ([]byte, error, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), vpnCoreCheckTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return output, err, ctx.Err() == context.DeadlineExceeded
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
