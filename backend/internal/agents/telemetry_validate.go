package agents

import (
	"fmt"
	"strings"
)

func validateHeartbeatTelemetry(t HeartbeatTelemetry) error {
	if t.SchemaVersion != 1 || t.CollectedAt.IsZero() {
		return fmt.Errorf("invalid telemetry envelope")
	}
	if strings.TrimSpace(t.VPNCore.Type) == "" || strings.TrimSpace(t.VPNCore.ServiceState) == "" {
		return fmt.Errorf("invalid VPN Core telemetry")
	}
	if t.Host.LogicalCPUs != nil && *t.Host.LogicalCPUs <= 0 {
		return fmt.Errorf("invalid logical CPU telemetry")
	}
	for _, value := range []*float64{t.Host.Load1, t.Host.Load5, t.Host.Load15} {
		if value != nil && *value < 0 {
			return fmt.Errorf("invalid load telemetry")
		}
	}
	if t.Host.MemoryTotalBytes != nil && *t.Host.MemoryTotalBytes == 0 {
		return fmt.Errorf("invalid total memory telemetry")
	}
	if t.Host.MemoryTotalBytes != nil && t.Host.MemoryAvailableBytes != nil && *t.Host.MemoryAvailableBytes > *t.Host.MemoryTotalBytes {
		return fmt.Errorf("invalid memory telemetry bounds")
	}
	if t.Host.RootFSTotalBytes != nil && *t.Host.RootFSTotalBytes == 0 {
		return fmt.Errorf("invalid filesystem telemetry")
	}
	if t.Host.RootFSTotalBytes != nil && t.Host.RootFSFreeBytes != nil && *t.Host.RootFSFreeBytes > *t.Host.RootFSTotalBytes {
		return fmt.Errorf("invalid filesystem telemetry bounds")
	}
	return nil
}
