package agents

import "testing"

func TestPlatformUpdateLifecycle(t *testing.T) {
	valid := [][2]string{
		{AgentOperationJobStatusPending, AgentOperationJobStatusInProgress},
		{AgentOperationJobStatusInProgress, AgentOperationJobStatusMutationDispatched},
		{AgentOperationJobStatusInProgress, AgentOperationJobStatusFailed},
		{AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusSucceeded},
		{AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusFailed},
		{AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusOutcomeUnknown},
	}
	for _, transition := range valid {
		if !ValidPlatformUpdateTransition(transition[0], transition[1]) {
			t.Fatalf("expected valid transition %q -> %q", transition[0], transition[1])
		}
	}

	invalid := [][2]string{
		{AgentOperationJobStatusPending, AgentOperationJobStatusSucceeded},
		{AgentOperationJobStatusPending, AgentOperationJobStatusMutationDispatched},
		{AgentOperationJobStatusInProgress, AgentOperationJobStatusSucceeded},
		{AgentOperationJobStatusInProgress, AgentOperationJobStatusOutcomeUnknown},
		{AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusPending},
		{AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusInProgress},
		{AgentOperationJobStatusSucceeded, AgentOperationJobStatusInProgress},
		{AgentOperationJobStatusFailed, AgentOperationJobStatusInProgress},
		{AgentOperationJobStatusOutcomeUnknown, AgentOperationJobStatusInProgress},
	}
	for _, transition := range invalid {
		if ValidPlatformUpdateTransition(transition[0], transition[1]) {
			t.Fatalf("expected invalid transition %q -> %q", transition[0], transition[1])
		}
	}
}

func TestMutationDispatchedRemainsActiveAndNonTerminal(t *testing.T) {
	if !PlatformUpdateStatusIsActive(AgentOperationJobStatusMutationDispatched) {
		t.Fatal("mutation_dispatched must remain active for per-node concurrency protection")
	}
	if PlatformUpdateStatusIsTerminal(AgentOperationJobStatusMutationDispatched) {
		t.Fatal("mutation_dispatched must not be terminal")
	}
}

func TestPlatformUpdateTerminalStatusesNeverBecomeActive(t *testing.T) {
	for _, status := range []string{
		AgentOperationJobStatusSucceeded,
		AgentOperationJobStatusFailed,
		AgentOperationJobStatusOutcomeUnknown,
	} {
		if !PlatformUpdateStatusIsTerminal(status) {
			t.Fatalf("expected %q to be terminal", status)
		}
		if PlatformUpdateStatusIsActive(status) {
			t.Fatalf("terminal status %q must not be active", status)
		}
	}
}

func TestMutationDispatchedCannotRedispatchAfterRestart(t *testing.T) {
	if ValidPlatformUpdateTransition(AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusInProgress) {
		t.Fatal("mutation_dispatched must never transition back to in_progress")
	}
	if ValidPlatformUpdateTransition(AgentOperationJobStatusMutationDispatched, AgentOperationJobStatusMutationDispatched) {
		t.Fatal("mutation_dispatched must never be re-dispatched")
	}
}
