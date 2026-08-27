package agents

import "testing"

func TestPlatformUpdateRolloutTransitionsAreMonotonic(t *testing.T) {
	allowed := [][2]PlatformUpdateRolloutStatus{
		{PlatformUpdateRolloutPending, PlatformUpdateRolloutRunning},
		{PlatformUpdateRolloutPending, PlatformUpdateRolloutFailed},
		{PlatformUpdateRolloutRunning, PlatformUpdateRolloutSucceeded},
		{PlatformUpdateRolloutRunning, PlatformUpdateRolloutFailed},
		{PlatformUpdateRolloutRunning, PlatformUpdateRolloutOutcomeUnknown},
	}
	for _, transition := range allowed {
		if err := validatePlatformUpdateRolloutTransition(transition[0], transition[1]); err != nil {
			t.Fatalf("allowed rollout transition %s -> %s rejected: %v", transition[0], transition[1], err)
		}
	}

	for _, terminal := range []PlatformUpdateRolloutStatus{
		PlatformUpdateRolloutSucceeded,
		PlatformUpdateRolloutFailed,
		PlatformUpdateRolloutOutcomeUnknown,
	} {
		if err := validatePlatformUpdateRolloutTransition(terminal, PlatformUpdateRolloutRunning); err == nil {
			t.Fatalf("terminal rollout state %s became runnable again", terminal)
		}
	}
}

func TestPlatformUpdateRolloutEntryRequiresProvenResultBeforeAdvance(t *testing.T) {
	for _, status := range []PlatformUpdateRolloutEntryStatus{
		PlatformUpdateRolloutEntryQueued,
		PlatformUpdateRolloutEntryWaiting,
		PlatformUpdateRolloutEntryUpdating,
		PlatformUpdateRolloutEntryFailed,
		PlatformUpdateRolloutEntryOutcomeUnknown,
	} {
		if rolloutMayAdvancePastEntry(status) {
			t.Fatalf("rollout advanced past unproven entry status %s", status)
		}
	}
	if !rolloutMayAdvancePastEntry(PlatformUpdateRolloutEntryHealthy) {
		t.Fatal("healthy entry did not permit advancement")
	}
	if !rolloutMayAdvancePastEntry(PlatformUpdateRolloutEntrySkipped) {
		t.Fatal("explicitly skipped planning blocker did not permit bounded advancement")
	}
}

func TestPlatformUpdateRolloutStopsOnFailedOrUnknownEntry(t *testing.T) {
	if !rolloutStopsForEntryStatus(PlatformUpdateRolloutEntryFailed) {
		t.Fatal("failed entry did not stop rollout")
	}
	if !rolloutStopsForEntryStatus(PlatformUpdateRolloutEntryOutcomeUnknown) {
		t.Fatal("outcome_unknown entry did not stop rollout")
	}
	for _, status := range []PlatformUpdateRolloutEntryStatus{
		PlatformUpdateRolloutEntryQueued,
		PlatformUpdateRolloutEntryWaiting,
		PlatformUpdateRolloutEntryUpdating,
		PlatformUpdateRolloutEntryHealthy,
		PlatformUpdateRolloutEntrySkipped,
	} {
		if rolloutStopsForEntryStatus(status) {
			t.Fatalf("non-blocking entry status %s unexpectedly stopped rollout", status)
		}
	}
}

func TestPlatformUpdateRolloutEntryCannotReplayMutation(t *testing.T) {
	for _, terminal := range []PlatformUpdateRolloutEntryStatus{
		PlatformUpdateRolloutEntryHealthy,
		PlatformUpdateRolloutEntryFailed,
		PlatformUpdateRolloutEntryOutcomeUnknown,
		PlatformUpdateRolloutEntrySkipped,
	} {
		if err := validatePlatformUpdateRolloutEntryTransition(terminal, PlatformUpdateRolloutEntryUpdating); err == nil {
			t.Fatalf("terminal rollout entry %s became mutation-runnable again", terminal)
		}
	}
	if err := validatePlatformUpdateRolloutEntryTransition(PlatformUpdateRolloutEntryUpdating, PlatformUpdateRolloutEntryWaiting); err == nil {
		t.Fatal("updating entry regressed to waiting and could be redispatched")
	}
}
