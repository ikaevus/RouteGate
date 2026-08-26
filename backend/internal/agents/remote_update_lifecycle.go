package agents

// Remote VPN-node software updates use a stricter lifecycle than ordinary
// synchronous Agent operations. A successful dispatch only proves that the
// detached worker was accepted; it does not prove that host mutation
// succeeded. Terminal state is therefore allowed only after deterministic
// pre-dispatch failure or durable receipt reconciliation.
const (
	AgentTaskKindPlatformUpdate       = "platform_update"
	PlatformUpdateOperationDispatch  = "dispatch"
	PlatformUpdateOperationReconcile = "reconcile"

	AgentOperationJobStatusMutationDispatched = "mutation_dispatched"
	AgentOperationJobStatusOutcomeUnknown     = "outcome_unknown"
)

// PlatformUpdateStatusIsTerminal reports whether no further mutation or
// reconciliation work is permitted for a remote update job.
func PlatformUpdateStatusIsTerminal(status string) bool {
	switch status {
	case AgentOperationJobStatusSucceeded,
		AgentOperationJobStatusFailed,
		AgentOperationJobStatusOutcomeUnknown:
		return true
	default:
		return false
	}
}

// PlatformUpdateStatusIsActive reports whether the job must continue to block
// a second mutating operation for the same VPN node. In particular,
// mutation_dispatched remains active until a durable Agent receipt is
// reconciled to a terminal state.
func PlatformUpdateStatusIsActive(status string) bool {
	switch status {
	case AgentOperationJobStatusPending,
		AgentOperationJobStatusInProgress,
		AgentOperationJobStatusMutationDispatched:
		return true
	default:
		return false
	}
}

// ValidPlatformUpdateTransition is the single Manager-side state-machine
// contract for remote VPN-node updates.
//
// A deterministic failure before detached launcher start may terminate from
// in_progress. Once durable handoff may have happened, recovery is
// reconciliation-only. There is deliberately no transition back to pending or
// another dispatch-capable state.
func ValidPlatformUpdateTransition(from, to string) bool {
	switch from {
	case AgentOperationJobStatusPending:
		return to == AgentOperationJobStatusInProgress
	case AgentOperationJobStatusInProgress:
		return to == AgentOperationJobStatusMutationDispatched ||
			to == AgentOperationJobStatusFailed
	case AgentOperationJobStatusMutationDispatched:
		return to == AgentOperationJobStatusSucceeded ||
			to == AgentOperationJobStatusFailed ||
			to == AgentOperationJobStatusOutcomeUnknown
	default:
		return false
	}
}
