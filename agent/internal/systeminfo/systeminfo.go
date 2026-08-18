package systeminfo

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ikaevus/routegate/agent/internal/buildinfo"
	"github.com/ikaevus/routegate/agent/internal/platform"
	"github.com/ikaevus/routegate/agent/internal/vpncoreinstall"
)

const (
	vpnCoreCheckTimeout              = 3 * time.Second
	telemetrySchemaVersion           = 1
	platformCapabilitySchemaVersion = platform.CapabilitySchemaVersion
)

type RuntimeMetrics struct {
	Load1       float64   `json:"load1"`
	Load5       float64   `json:"load5"`
	Load15      float64   `json:"load15"`
	LogicalCPUs int       `json:"logicalCpus"`
	CollectedAt time.Time `json:"collectedAt"`
}

type TelemetrySnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	CollectedAt   time.Time        `json:"collectedAt"`
	Host          HostTelemetry    `json:"host"`
	VPNCore       VPNCoreTelemetry `json:"vpnCore"`
}

type HostTelemetry struct {
	Load1               *float64 `json:"load1,omitempty"`
	Load5               *float64 `json:"load5,omitempty"`
	Load15              *float64 `json:"load15,omitempty"`
	LogicalCPUs          *int     `json:"logicalCpus,omitempty"`
	MemoryTotalBytes     *uint64  `json:"memoryTotalBytes,omitempty"`
	MemoryAvailableBytes *uint64  `json:"memoryAvailableBytes,omitempty"`
	RootFSTotalBytes     *uint64  `json:"rootFsTotalBytes,omitempty"`
	RootFSFreeBytes      *uint64  `json:"rootFsFreeBytes,omitempty"`
	UptimeSeconds        *uint64  `json:"uptimeSeconds,omitempty"`
}

type VPNCoreTelemetry struct {
	Type         string `json:"type"`
	Installed    bool   `json:"installed"`
	Version      string `json:"version,omitempty"`
	ServiceState string `json:"serviceState"`
}

type Info struct {
	Hostname        string             `json:"hostname"`
	AgentVersion    string             `json:"agentVersion"`
	ProtocolVersion int                `json:"protocolVersion"`
	OS              string             `json:"os"`
	Arch            string             `json:"arch"`
	Capabilities    map[string]any     `json:"capabilities"`
	RuntimeMetrics  *RuntimeMetrics    `json:"runtimeMetrics,omitempty"`
	Telemetry       *TelemetrySnapshot `json:"telemetry,omitempty"`
}

func Collect() Info {
	hostname, _ := os.Hostname()
	info := buildinfo.Current()
	runtimeMetrics := collectRuntimeMetrics()
	vpnCore := detectVPNCore()
	wireGuardCore := detectWireGuardCore()
	return Info{
		Hostname:        hostname,
		AgentVersion:    info.Version,
		ProtocolVersion: info.ProtocolVersion,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Capabilities:    detectCapabilitiesWithWireGuard(vpnCore, wireGuardCore),
		RuntimeMetrics:  runtimeMetrics,
		Telemetry:       collectTelemetry(runtimeMetrics, vpnCore),
	}
}

func collectRuntimeMetrics() *RuntimeMetrics {
	if runtime.GOOS != "linux" {
		return nil
	}

	payload, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	load1, load5, load15, ok := parseLoadAverage(payload)
	if !ok {
		return nil
	}
	logicalCPUs := runtime.NumCPU()
	if logicalCPUs <= 0 {
		return nil
	}

	return &RuntimeMetrics{
		Load1:       load1,
		Load5:       load5,
		Load15:      load15,
		LogicalCPUs: logicalCPUs,
		CollectedAt: time.Now().UTC(),
	}
}

func collectTelemetry(runtimeMetrics *RuntimeMetrics, vpnCore map[string]any) *TelemetrySnapshot {
	now := time.Now().UTC()
	host := HostTelemetry{}
	if runtimeMetrics != nil {
		host.Load1 = float64Pointer(runtimeMetrics.Load1)
		host.Load5 = float64Pointer(runtimeMetrics.Load5)
		host.Load15 = float64Pointer(runtimeMetrics.Load15)
		host.LogicalCPUs = intPointer(runtimeMetrics.LogicalCPUs)
	}

	if runtime.GOOS == "linux" {
		if payload, err := os.ReadFile("/proc/meminfo"); err == nil {
			if total, available, ok := parseMemoryInfo(payload); ok {
				host.MemoryTotalBytes = uint64Pointer(total)
				host.MemoryAvailableBytes = uint64Pointer(available)
			}
		}
		if payload, err := os.ReadFile("/proc/uptime"); err == nil {
			if uptime, ok := parseUptime(payload); ok {
				host.UptimeSeconds = uint64Pointer(uptime)
			}
		}
		if total, free, ok := collectRootFilesystem(); ok {
			host.RootFSTotalBytes = uint64Pointer(total)
			host.RootFSFreeBytes = uint64Pointer(free)
		}
	}

	return &TelemetrySnapshot{
		SchemaVersion: telemetrySchemaVersion,
		CollectedAt:   now,
		Host:          host,
		VPNCore:       telemetryVPNCore(vpnCore),
	}
}

func parseLoadAverage(payload []byte) (float64, float64, float64, bool) {
	fields := strings.Fields(string(payload))
	if len(fields) < 3 {
		return 0, 0, 0, false
	}

	values := make([]float64, 3)
	for index := 0; index < 3; index++ {
		value, err := strconv.ParseFloat(fields[index], 64)
		if err != nil || value < 0 {
			return 0, 0, 0, false
		}
		values[index] = value
	}

	return values[0], values[1], values[2], true
}

func parseMemoryInfo(payload []byte) (uint64, uint64, bool) {
	var totalKB uint64
	var availableKB uint64
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = value
		case "MemAvailable:":
			availableKB = value
		}
	}
	if totalKB == 0 || availableKB > totalKB {
		return 0, 0, false
	}
	return totalKB * 1024, availableKB * 1024, true
}

func parseUptime(payload []byte) (uint64, bool) {
	fields := strings.Fields(string(payload))
	if len(fields) == 0 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return uint64(seconds), true
}

func collectRootFilesystem() (uint64, uint64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil || stat.Bsize <= 0 {
		return 0, 0, false
	}
	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	free := stat.Bavail * blockSize
	if total == 0 || free > total {
		return 0, 0, false
	}
	return total, free, true
}

func DetectCapabilities() map[string]any {
	return detectCapabilitiesWithWireGuard(detectVPNCore(), detectWireGuardCore())
}

func detectCapabilities(vpnCore map[string]any) map[string]any {
	return detectCapabilitiesWithWireGuard(vpnCore, detectWireGuardCore())
}

func detectCapabilitiesWithWireGuard(vpnCore, wireGuardCore map[string]any) map[string]any {
	names := []string{"systemctl", "sing-box", "xray", "nft"}
	caps := make(map[string]any, len(names)+4)
	for _, name := range names {
		_, err := exec.LookPath(name)
		caps[name] = err == nil
	}
	caps["vpnCore"] = vpnCore
	caps["vpnCores"] = []map[string]any{vpnCore, wireGuardCore}
	caps["routegate"] = routeGatePlatformCapabilities()
	caps["vpnCoreServiceOperations"] = []string{"start", "stop", "restart"}
	if vpncoreinstall.SupportsCurrentPlatform() {
		caps["vpnCoreInstallationOperations"] = []string{vpncoreinstall.OperationInstall}
	}
	return caps
}

func detectWireGuardCore() map[string]any {
	status := map[string]any{
		"type": "wireguard", "installed": false, "state": "not_installed",
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
	}
	wgPath, wgErr := exec.LookPath("wg")
	wgQuickPath, wgQuickErr := exec.LookPath("wg-quick")
	if wgErr != nil || wgQuickErr != nil {
		return status
	}
	status["installed"] = true
	status["binaryPath"] = wgPath
	status["helperPath"] = wgQuickPath
	status["state"] = "installed"
	if _, systemctlErr := exec.LookPath("systemctl"); systemctlErr != nil {
		status["serviceState"] = "unknown"
		return status
	}
	serviceOutput, serviceErr, timedOut := runCommand("systemctl", "is-active", "wg-quick@routegate-wg0")
	serviceState := strings.TrimSpace(string(serviceOutput))
	if timedOut {
		serviceState = "unknown"
		status["serviceError"] = "service_check_timeout"
	} else if serviceState == "" {
		serviceState = "unknown"
	}
	status["serviceName"] = "wg-quick@routegate-wg0.service"
	status["serviceState"] = serviceState
	switch serviceState {
	case "active": status["state"] = "running"
	case "inactive", "deactivating": status["state"] = "stopped"
	case "failed": status["state"] = "failed"
	default:
		if serviceErr != nil { status["state"] = "unknown" }
	}
	return status
}

// routeGatePlatformCapabilities reports what this Agent build can manage. It
// intentionally differs from binary detection: an installed VPN Core may
// support more protocols than RouteGate has safe render/apply adapters for.
func routeGatePlatformCapabilities() map[string]any {
	managedAdapters := platform.ManagedVPNCoreAdapters()
	adapterCapabilities := make([]map[string]any, 0, len(managedAdapters))
	for _, adapter := range managedAdapters {
		adapterCapabilities = append(adapterCapabilities, adapter.CapabilityMap())
	}
	return map[string]any{
		"schemaVersion":    platformCapabilitySchemaVersion,
		"nodeCapabilities": []string{"vpn"},
		"vpnCoreAdapters":  adapterCapabilities,
	}
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

func telemetryVPNCore(status map[string]any) VPNCoreTelemetry {
	core := VPNCoreTelemetry{Type: "sing-box", ServiceState: "unknown"}
	if value, ok := status["type"].(string); ok && strings.TrimSpace(value) != "" {
		core.Type = strings.TrimSpace(value)
	}
	if value, ok := status["installed"].(bool); ok {
		core.Installed = value
	}
	if value, ok := status["version"].(string); ok {
		core.Version = strings.TrimSpace(value)
	}
	if value, ok := status["serviceState"].(string); ok && strings.TrimSpace(value) != "" {
		core.ServiceState = strings.TrimSpace(value)
	} else if !core.Installed {
		core.ServiceState = "not_installed"
	}
	return core
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

func float64Pointer(value float64) *float64 { return &value }
func intPointer(value int) *int             { return &value }
func uint64Pointer(value uint64) *uint64    { return &value }
