package agents

import "fmt"

type PlatformUpdateRolloutStatus string

type PlatformUpdateRolloutEntryStatus string

const (
	PlatformUpdateRolloutPending        PlatformUpdateRolloutStatus = "pending"
	PlatformUpdateRolloutRunning        PlatformUpdateRolloutStatus = "running"
	PlatformUpdateRolloutSucceeded      PlatformUpdateRolloutStatus = "succeeded"
	PlatformUpdateRolloutFailed         PlatformUpdateRolloutStatus = "failed"
	PlatformUpdateRolloutOutcomeUnknown PlatformUpdateRolloutStatus = "outcome_unknown"

	PlatformUpdateRolloutEntryQueued         PlatformUpdateRolloutEntryStatus = "queued"
	PlatformUpdateRolloutEntryWaiting        PlatformUpdateRolloutEntryStatus = "waiting"
	PlatformUpdateRolloutEntryUpdating       PlatformUpdateRolloutEntryStatus = "updating"
	PlatformUpdateRolloutEntryHealthy        PlatformUpdateRolloutEntryStatus = "healthy"
	PlatformUpdateRolloutEntryFailed         PlatformUpdateRolloutEntryStatus = "failed"
	PlatformUpdateRolloutEntryOutcomeUnknown PlatformUpdateRolloutEntryStatus = "outcome_unknown"
	PlatformUpdateRolloutEntrySkipped        PlatformUpdateRolloutEntryStatus = "skipped"
)

func validatePlatformUpdateRolloutTransition(from, to PlatformUpdateRolloutStatus) error {
	allowed := false
	switch from {
	case PlatformUpdateRolloutPending:
		allowed = to == PlatformUpdateRolloutRunning || to == PlatformUpdateRolloutFailed
	case PlatformUpdateRolloutRunning:
		allowed = to == PlatformUpdateRolloutSucceeded || to == PlatformUpdateRolloutFailed || to == PlatformUpdateRolloutOutcomeUnknown
	}
	if !allowed {
		return fmt.Errorf("invalid platform update rollout transition %s -> %s", from, to)
	}
	return nil
}

func validatePlatformUpdateRolloutEntryTransition(from, to PlatformUpdateRolloutEntryStatus) error {
	allowed := false
	switch from {
	case PlatformUpdateRolloutEntryQueued:
		allowed = to == PlatformUpdateRolloutEntryWaiting || to == PlatformUpdateRolloutEntryUpdating || to == PlatformUpdateRolloutEntrySkipped
	case PlatformUpdateRolloutEntryWaiting:
		allowed = to == PlatformUpdateRolloutEntryUpdating || to == PlatformUpdateRolloutEntrySkipped
	case PlatformUpdateRolloutEntryUpdating:
		allowed = to == PlatformUpdateRolloutEntryHealthy || to == PlatformUpdateRolloutEntryFailed || to == PlatformUpdateRolloutEntryOutcomeUnknown
	}
	if !allowed {
		return fmt.Errorf("invalid platform update rollout entry transition %s -> %s", from, to)
	}
	return nil
}

func rolloutStopsForEntryStatus(status PlatformUpdateRolloutEntryStatus) bool {
	return status == PlatformUpdateRolloutEntryFailed || status == PlatformUpdateRolloutEntryOutcomeUnknown
}

func rolloutMayAdvancePastEntry(status PlatformUpdateRolloutEntryStatus) bool {
	return status == PlatformUpdateRolloutEntryHealthy || status == PlatformUpdateRolloutEntrySkipped
}
