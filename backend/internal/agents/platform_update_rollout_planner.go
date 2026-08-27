package agents

import (
	"fmt"
	"strings"
)

// PlatformUpdateRolloutBlocker is a bounded planning reason retained for
// administrator-visible diagnostics. Planner callers must not substitute
// arbitrary free-form privileged selectors for these values.
type PlatformUpdateRolloutBlocker string

const (
	PlatformUpdateRolloutBlockerManagerVersionMismatch PlatformUpdateRolloutBlocker = "manager_version_mismatch"
	PlatformUpdateRolloutBlockerNotVPNRole             PlatformUpdateRolloutBlocker = "not_vpn_role"
	PlatformUpdateRolloutBlockerServerDisabled         PlatformUpdateRolloutBlocker = "server_disabled"
	PlatformUpdateRolloutBlockerAgentMissing           PlatformUpdateRolloutBlocker = "agent_missing"
	PlatformUpdateRolloutBlockerAgentDisabled          PlatformUpdateRolloutBlocker = "agent_disabled"
	PlatformUpdateRolloutBlockerUpdateCapability       PlatformUpdateRolloutBlocker = "update_capability_not_ready"
	PlatformUpdateRolloutBlockerActiveUpdate           PlatformUpdateRolloutBlocker = "active_or_unresolved_update"
	PlatformUpdateRolloutBlockerProtocolIncompatible   PlatformUpdateRolloutBlocker = "agent_protocol_incompatible"
)

// PlatformUpdateRolloutCandidate contains only Manager-owned inventory and
// observed Agent facts needed to plan a VPN-node rollout. It deliberately has
// no updater path, URL, checksum, signer, trust-root, command, environment, or
// Agent-identity selector fields.
type PlatformUpdateRolloutCandidate struct {
	ServerID                    string
	DeploymentRole              string
	ServerDisabled              bool
	AgentRegistered             bool
	AgentDisabled               bool
	UpdateCapabilityReady       bool
	HasActiveOrUnresolvedUpdate bool
	AgentProtocolCompatible     bool
}

type PlatformUpdateRolloutPlanEntry struct {
	ServerID string
	Eligible bool
	Blockers []PlatformUpdateRolloutBlocker
}

type PlatformUpdateRolloutPlan struct {
	TargetVersion string
	Entries       []PlatformUpdateRolloutPlanEntry
}

// PlanPlatformUpdateRollout evaluates candidates in caller-supplied order and
// never silently drops an ineligible requested node. Management-first is a
// rollout-wide fail-closed gate: when Manager is not already on targetVersion,
// every requested candidate retains the manager-version blocker.
func PlanPlatformUpdateRollout(managerVersion, targetVersion string, candidates []PlatformUpdateRolloutCandidate) (PlatformUpdateRolloutPlan, error) {
	if !validPlatformUpdateTargetVersion(targetVersion) {
		return PlatformUpdateRolloutPlan{}, fmt.Errorf("invalid target version")
	}
	if len(candidates) == 0 {
		return PlatformUpdateRolloutPlan{}, fmt.Errorf("at least one rollout candidate is required")
	}

	plan := PlatformUpdateRolloutPlan{
		TargetVersion: targetVersion,
		Entries:       make([]PlatformUpdateRolloutPlanEntry, 0, len(candidates)),
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		serverID := strings.TrimSpace(candidate.ServerID)
		if serverID == "" {
			return PlatformUpdateRolloutPlan{}, fmt.Errorf("candidate server id is required")
		}
		if _, exists := seen[serverID]; exists {
			return PlatformUpdateRolloutPlan{}, fmt.Errorf("duplicate rollout candidate server id %q", serverID)
		}
		seen[serverID] = struct{}{}

		entry := PlatformUpdateRolloutPlanEntry{ServerID: serverID}
		if managerVersion != targetVersion {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerManagerVersionMismatch)
		}
		if candidate.DeploymentRole != "vpn" {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerNotVPNRole)
		}
		if candidate.ServerDisabled {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerServerDisabled)
		}
		if !candidate.AgentRegistered {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerAgentMissing)
		} else if candidate.AgentDisabled {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerAgentDisabled)
		}
		if !candidate.UpdateCapabilityReady {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerUpdateCapability)
		}
		if candidate.HasActiveOrUnresolvedUpdate {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerActiveUpdate)
		}
		if !candidate.AgentProtocolCompatible {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerProtocolIncompatible)
		}
		entry.Eligible = len(entry.Blockers) == 0
		plan.Entries = append(plan.Entries, entry)
	}
	return plan, nil
}
