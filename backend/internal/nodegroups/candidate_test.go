package nodegroups

import (
	"testing"
	"time"
)

func TestEvaluateCandidateReportsReadyAndHighLoadWithoutSelecting(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	lastSeen := now.Add(-30 * time.Second)
	load, cpus := 6.0, 4
	candidate := evaluateCandidate(NodeGroupCandidate{
		MemberEnabled: true,
		NodeStatus: "active",
		AgentStatus: "online",
		LastSeenAt: &lastSeen,
		ProtocolSupported: true,
		RuntimeState: "running",
		Load1: &load,
		LogicalCPUs: &cpus,
	}, "vpn", now)
	if !candidate.Eligible || candidate.Health != CandidateHealthDegraded {
		t.Fatalf("unexpected candidate: %+v", candidate)
	}
	if len(candidate.Signals) != 1 || candidate.Signals[0] != "high_load" {
		t.Fatalf("unexpected signals: %+v", candidate.Signals)
	}
}

func TestEvaluateCandidateExplainsUnavailableNode(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	candidate := evaluateCandidate(NodeGroupCandidate{
		MemberEnabled: true,
		NodeStatus: "active",
		ProtocolSupported: false,
	}, "vpn", now)
	if candidate.Eligible || candidate.Health != CandidateHealthUnavailable {
		t.Fatalf("unexpected candidate: %+v", candidate)
	}
	want := map[string]bool{"agent_missing": true, "heartbeat_stale": true, "protocol_unsupported": true, "runtime_not_reported": true}
	for _, signal := range candidate.Signals {
		delete(want, signal)
	}
	if len(want) != 0 {
		t.Fatalf("missing signals: %+v in %+v", want, candidate.Signals)
	}
}

func TestCapabilitiesSupportProtocolUsesVersionedAdapterContract(t *testing.T) {
	data := []byte(`{"vpnCores":[{"type":"wireguard","state":"running"}],"routegate":{"schemaVersion":1,"vpnCoreAdapters":[{"core":"sing-box","protocol":"vless"},{"core":"wireguard","protocol":"wireguard"}]}}`)
	supported, state := capabilityEvidence(data, "wireguard")
	if !supported || state != "running" {
		t.Fatal("expected WireGuard support")
	}
	supported, _ = capabilityEvidence(data, "hysteria2")
	if supported {
		t.Fatal("unexpected Hysteria2 support")
	}
}
