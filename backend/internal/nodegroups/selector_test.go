package nodegroups

import "testing"

func TestSelectCandidatePriorityPrefersReadyThenLowestPriority(t *testing.T) {
	selection, ok := SelectCandidate("account-1", SelectionStrategyPriority, []NodeGroupCandidate{
		{ServerID: "degraded", Priority: 1, Eligible: true, Health: CandidateHealthDegraded},
		{ServerID: "ready-high", Priority: 20, Eligible: true, Health: CandidateHealthReady},
		{ServerID: "ready-low", Priority: 10, Eligible: true, Health: CandidateHealthReady},
	}, true)
	if !ok || selection.Candidate.ServerID != "ready-low" {
		t.Fatalf("unexpected selection: ok=%v selection=%+v", ok, selection)
	}
}

func TestSelectCandidateNeverSelectsUnavailableCandidate(t *testing.T) {
	selection, ok := SelectCandidate("account-1", SelectionStrategyPriority, []NodeGroupCandidate{
		{ServerID: "unavailable-best-priority", Priority: 1, Weight: 1000, Eligible: false, Health: CandidateHealthUnavailable},
		{ServerID: "ready", Priority: 100, Weight: 1, Eligible: true, Health: CandidateHealthReady},
	}, true)
	if !ok || selection.Candidate.ServerID != "ready" {
		t.Fatalf("unavailable candidate influenced selection: ok=%v selection=%+v", ok, selection)
	}

	if _, ok := SelectCandidate("account-1", SelectionStrategyWeighted, []NodeGroupCandidate{
		{ServerID: "unavailable-only", Priority: 1, Weight: 1000, Eligible: false, Health: CandidateHealthUnavailable},
	}, true); ok {
		t.Fatal("unavailable candidate was selected when it was the only candidate")
	}
}

func TestSelectCandidateUsesDegradedOnlyWhenAllowed(t *testing.T) {
	candidates := []NodeGroupCandidate{{ServerID: "degraded", Weight: 10, Eligible: true, Health: CandidateHealthDegraded}}
	if _, ok := SelectCandidate("account-1", SelectionStrategyWeighted, candidates, false); ok {
		t.Fatal("degraded candidate selected without explicit policy")
	}
	selection, ok := SelectCandidate("account-1", SelectionStrategyWeighted, candidates, true)
	if !ok || selection.Candidate.ServerID != "degraded" {
		t.Fatalf("unexpected degraded fallback: ok=%v selection=%+v", ok, selection)
	}
}

func TestSelectCandidateWeightedIsStableAndWeightSensitive(t *testing.T) {
	candidates := []NodeGroupCandidate{
		{ServerID: "node-a", Weight: 1, Eligible: true, Health: CandidateHealthReady},
		{ServerID: "node-b", Weight: 1000, Eligible: true, Health: CandidateHealthReady},
	}
	first, ok := SelectCandidate("stable-account", SelectionStrategyWeighted, candidates, false)
	if !ok {
		t.Fatal("expected a weighted selection")
	}
	second, _ := SelectCandidate("stable-account", SelectionStrategyWeighted, candidates, false)
	if first.Candidate.ServerID != second.Candidate.ServerID {
		t.Fatalf("weighted selection changed: %s != %s", first.Candidate.ServerID, second.Candidate.ServerID)
	}
	if weightedRendezvousScore("stable-account", "node-a", 100) >= weightedRendezvousScore("stable-account", "node-a", 1) {
		t.Fatal("higher weight must improve a candidate's rendezvous score")
	}
}
