package diagnostics

import (
	"fmt"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/systeminfo"
)

const (
	ProfileHostOverview = "host_overview"
	ProfileVPNCoreStatus = "vpn_core_status"
	SchemaVersion        = 1
)

func ValidProfile(profileKey string) bool {
	switch strings.TrimSpace(profileKey) {
	case ProfileHostOverview, ProfileVPNCoreStatus:
		return true
	default:
		return false
	}
}

// Execute runs one compile-time allow-listed diagnostic collector. There is no
// command, args, script, or arbitrary shell input in the diagnostic protocol.
func Execute(profileKey string) (map[string]any, error) {
	profileKey = strings.TrimSpace(profileKey)
	if !ValidProfile(profileKey) {
		return nil, fmt.Errorf("unsupported diagnostic profile %q", profileKey)
	}

	info := systeminfo.Collect()
	result := map[string]any{
		"schemaVersion": SchemaVersion,
		"profileKey":    profileKey,
		"collectedAt":   time.Now().UTC(),
	}
	if info.Telemetry == nil {
		result["evidence"] = map[string]any{"available": false}
		return result, nil
	}

	switch profileKey {
	case ProfileHostOverview:
		result["evidence"] = map[string]any{
			"available": true,
			"hostname":  info.Hostname,
			"os":        info.OS,
			"arch":      info.Arch,
			"host":      info.Telemetry.Host,
		}
	case ProfileVPNCoreStatus:
		result["evidence"] = map[string]any{
			"available": true,
			"vpnCore":   info.Telemetry.VPNCore,
		}
	}
	return result, nil
}
