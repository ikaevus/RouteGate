package agents

import (
	"reflect"
	"testing"
)

const (
	rolloutServerA = "11111111-1111-4111-8111-111111111111"
	rolloutServerB = "22222222-2222-4222-8222-222222222222"
	rolloutServerC = "33333333-3333-4333-8333-333333333333"
)

func eligibleRolloutCandidate(id string) PlatformUpdateRolloutCandidate {
	return PlatformUpdateRolloutCandidate{
		ServerID:                id,
		DeploymentRole:          "vpn",
		AgentRegistered:         true,
		UpdateCapabilityReady:   true,
		AgentProtocolCompatible: true,
	}
}

func TestPlanPlatformUpdateRolloutPreservesRequestedOrder(t *testing.T) {
	plan, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{
		eligibleRolloutCandidate(rolloutServerB),
		eligibleRolloutCandidate(rolloutServerA),
	})
	if err != nil {
		t.Fatalf("plan rollout: %v", err)
	}
	got := []string{plan.Entries[0].ServerID, plan.Entries[1].ServerID}
	want := []string{rolloutServerB, rolloutServerA}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry order = %v, want %v", got, want)
	}
	for _, entry := range plan.Entries {
		if !entry.Eligible || len(entry.Blockers) != 0 {
			t.Fatalf("entry %+v unexpectedly blocked", entry)
		}
	}
}

func TestPlanPlatformUpdateRolloutFailsClosedWhenManagerNotOnTarget(t *testing.T) {
	plan, err := PlanPlatformUpdateRollout("1.2.2", "1.2.3", []PlatformUpdateRolloutCandidate{
		eligibleRolloutCandidate(rolloutServerA),
		eligibleRolloutCandidate(rolloutServerB),
	})
	if err != nil {
		t.Fatalf("plan rollout: %v", err)
	}
	for _, entry := range plan.Entries {
		if entry.Eligible {
			t.Fatalf("entry %+v unexpectedly eligible", entry)
		}
		if !reflect.DeepEqual(entry.Blockers, []PlatformUpdateRolloutBlocker{PlatformUpdateRolloutBlockerManagerVersionMismatch}) {
			t.Fatalf("blockers = %v", entry.Blockers)
		}
	}
}

func TestPlanPlatformUpdateRolloutRejectsNonVPNAndPreservesBlockers(t *testing.T) {
	candidate := eligibleRolloutCandidate(rolloutServerA)
	candidate.DeploymentRole = "hybrid"
	candidate.ServerDisabled = true
	candidate.AgentDisabled = true
	candidate.UpdateCapabilityReady = false
	candidate.HasActiveOrUnresolvedUpdate = true
	candidate.AgentProtocolCompatible = false

	plan, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{candidate})
	if err != nil {
		t.Fatalf("plan rollout: %v", err)
	}
	want := []PlatformUpdateRolloutBlocker{
		PlatformUpdateRolloutBlockerNotVPNRole,
		PlatformUpdateRolloutBlockerServerDisabled,
		PlatformUpdateRolloutBlockerAgentDisabled,
		PlatformUpdateRolloutBlockerUpdateCapability,
		PlatformUpdateRolloutBlockerActiveUpdate,
		PlatformUpdateRolloutBlockerProtocolIncompatible,
	}
	if !reflect.DeepEqual(plan.Entries[0].Blockers, want) {
		t.Fatalf("blockers = %v, want %v", plan.Entries[0].Blockers, want)
	}
}

func TestPlanPlatformUpdateRolloutReportsMissingAgent(t *testing.T) {
	candidate := eligibleRolloutCandidate(rolloutServerA)
	candidate.AgentRegistered = false
	candidate.AgentDisabled = true

	plan, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{candidate})
	if err != nil {
		t.Fatalf("plan rollout: %v", err)
	}
	if got := plan.Entries[0].Blockers[0]; got != PlatformUpdateRolloutBlockerAgentMissing {
		t.Fatalf("first blocker = %q, want missing agent", got)
	}
	for _, blocker := range plan.Entries[0].Blockers {
		if blocker == PlatformUpdateRolloutBlockerAgentDisabled {
			t.Fatal("missing agent must not also report agent_disabled")
		}
	}
}

func TestPlanPlatformUpdateRolloutRejectsDuplicateOrEmptyIdentity(t *testing.T) {
	if _, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", nil); err == nil {
		t.Fatal("empty candidate set unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "", []PlatformUpdateRolloutCandidate{eligibleRolloutCandidate(rolloutServerA)}); err == nil {
		t.Fatal("empty target version unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "latest", []PlatformUpdateRolloutCandidate{eligibleRolloutCandidate(rolloutServerA)}); err == nil {
		t.Fatal("noncanonical target version unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", " 1.2.3 ", []PlatformUpdateRolloutCandidate{eligibleRolloutCandidate(rolloutServerA)}); err == nil {
		t.Fatal("whitespace-padded target version unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{{ServerID: "   "}}); err == nil {
		t.Fatal("empty server id unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{eligibleRolloutCandidate("not-a-uuid")}); err == nil {
		t.Fatal("non-UUID server id unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{
		eligibleRolloutCandidate(rolloutServerA),
		eligibleRolloutCandidate(rolloutServerA),
	}); err == nil {
		t.Fatal("duplicate server id unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{
		eligibleRolloutCandidate(rolloutServerA),
		eligibleRolloutCandidate(" 11111111-1111-4111-8111-111111111111 "),
	}); err == nil {
		t.Fatal("whitespace-equivalent duplicate server id unexpectedly accepted")
	}
	if _, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{
		eligibleRolloutCandidate(rolloutServerA),
		eligibleRolloutCandidate("11111111111141118111111111111111"),
	}); err == nil {
		t.Fatal("alternate UUID spelling unexpectedly bypassed duplicate detection")
	}
}

func TestPlanPlatformUpdateRolloutCanonicalizesServerID(t *testing.T) {
	plan, err := PlanPlatformUpdateRollout("1.2.3", "1.2.3", []PlatformUpdateRolloutCandidate{eligibleRolloutCandidate(" {11111111-1111-4111-8111-111111111111} ")})
	if err != nil {
		t.Fatalf("plan rollout: %v", err)
	}
	if got := plan.Entries[0].ServerID; got != rolloutServerA {
		t.Fatalf("server id = %q, want canonical %q", got, rolloutServerA)
	}
}

func TestCanonicalPlatformUpdateServerIDNormalizesCaseAndURN(t *testing.T) {
	got, err := canonicalPlatformUpdateServerID("URN:UUID:33333333-3333-4333-8333-333333333333")
	if err != nil {
		t.Fatalf("canonicalize UUID: %v", err)
	}
	if got != rolloutServerC {
		t.Fatalf("server id = %q, want %q", got, rolloutServerC)
	}
}
