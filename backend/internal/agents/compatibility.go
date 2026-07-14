package agents

import (
	"strconv"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

const (
	CompatibilityCompatible         = "compatible"
	CompatibilityUpgradeRecommended = "upgrade_recommended"
	CompatibilityUpgradeRequired    = "upgrade_required"
	CompatibilityUnsupported        = "unsupported"
	CompatibilityUnknown            = "unknown"
)

type Compatibility struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func EvaluateCompatibility(agentVersion string, protocolVersion *int) Compatibility {
	info := buildinfo.Current()
	return EvaluateCompatibilityWithPolicy(agentVersion, protocolVersion, info.MinimumAgentProtocolVersion, info.AgentProtocolVersion, info.RecommendedAgentVersion)
}

func EvaluateCompatibilityWithPolicy(agentVersion string, protocolVersion *int, minimumProtocolVersion, managerProtocolVersion int, recommendedAgentVersion string) Compatibility {
	if protocolVersion == nil {
		return Compatibility{Status: CompatibilityUnknown, Message: "agent has not reported a protocol version"}
	}
	if *protocolVersion < minimumProtocolVersion {
		return Compatibility{Status: CompatibilityUpgradeRequired, Message: "agent protocol is below the minimum supported version"}
	}
	if *protocolVersion > managerProtocolVersion {
		return Compatibility{Status: CompatibilityUnsupported, Message: "agent protocol is newer than this Manager supports"}
	}
	if olderThanRecommended(agentVersion, recommendedAgentVersion) {
		return Compatibility{Status: CompatibilityUpgradeRecommended, Message: "agent version is older than the recommended version"}
	}
	return Compatibility{Status: CompatibilityCompatible}
}

func olderThanRecommended(agentVersion, recommendedAgentVersion string) bool {
	agentParts, ok := parseDottedVersion(agentVersion)
	if !ok {
		return false
	}
	recommendedParts, ok := parseDottedVersion(recommendedAgentVersion)
	if !ok {
		return false
	}
	for len(agentParts) < len(recommendedParts) {
		agentParts = append(agentParts, 0)
	}
	for len(recommendedParts) < len(agentParts) {
		recommendedParts = append(recommendedParts, 0)
	}
	for i := range agentParts {
		if agentParts[i] < recommendedParts[i] {
			return true
		}
		if agentParts[i] > recommendedParts[i] {
			return false
		}
	}
	return false
}

func parseDottedVersion(value string) ([]int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if value == "" || strings.EqualFold(value, "dev") || strings.EqualFold(value, "unknown") {
		return nil, false
	}
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		out = append(out, number)
	}
	return out, true
}
