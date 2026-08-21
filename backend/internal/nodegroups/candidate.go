package nodegroups

import "time"

const candidateHeartbeatStaleAfter = 2 * time.Minute

func evaluateCandidate(candidate NodeGroupCandidate, deploymentRole string, now time.Time) NodeGroupCandidate {
	candidate.Signals = make([]string, 0)
	if !candidate.MemberEnabled {
		candidate.Signals = append(candidate.Signals, "member_disabled")
	}
	if deploymentRole != "vpn" && deploymentRole != "hybrid" {
		candidate.Signals = append(candidate.Signals, "not_vpn_node")
	}
	if candidate.NodeStatus != "active" {
		candidate.Signals = append(candidate.Signals, "node_not_active")
	}
	if candidate.AgentStatus == "" {
		candidate.Signals = append(candidate.Signals, "agent_missing")
	} else if candidate.AgentStatus != "online" {
		candidate.Signals = append(candidate.Signals, "agent_not_online")
	}
	if candidate.LastSeenAt == nil || now.Sub(candidate.LastSeenAt.UTC()) > candidateHeartbeatStaleAfter {
		candidate.Signals = append(candidate.Signals, "heartbeat_stale")
	}
	if candidate.TopologySupported != nil && !*candidate.TopologySupported {
		candidate.Signals = append(candidate.Signals, "protocol_topology_unsupported")
	}
	if !candidate.ProtocolSupported {
		candidate.Signals = append(candidate.Signals, "protocol_unsupported")
	}
	if candidate.RuntimeState == "" {
		candidate.Signals = append(candidate.Signals, "runtime_not_reported")
	} else if candidate.RuntimeState != "running" {
		candidate.Signals = append(candidate.Signals, "runtime_not_running")
	}

	candidate.Eligible = len(candidate.Signals) == 0
	if !candidate.Eligible {
		candidate.Health = CandidateHealthUnavailable
		return candidate
	}

	candidate.Health = CandidateHealthReady
	if candidate.Load1 != nil && candidate.LogicalCPUs != nil && *candidate.LogicalCPUs > 0 {
		loadPerCPU := *candidate.Load1 / float64(*candidate.LogicalCPUs)
		candidate.LoadPerCPU = &loadPerCPU
		if loadPerCPU >= 1 {
			candidate.Health = CandidateHealthDegraded
			candidate.Signals = append(candidate.Signals, "high_load")
		}
	}
	return candidate
}
