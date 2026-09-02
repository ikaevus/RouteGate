package agents

import (
	"context"
	"errors"
	"fmt"
)

type PlatformUpdateRolloutStepAction string

const (
	PlatformUpdateRolloutStepMutationAdmitted   PlatformUpdateRolloutStepAction = "mutation_admitted"
	PlatformUpdateRolloutStepMutationInProgress PlatformUpdateRolloutStepAction = "mutation_in_progress"
	PlatformUpdateRolloutStepWaitingHealth      PlatformUpdateRolloutStepAction = "waiting_health"
	PlatformUpdateRolloutStepNodeHealthy        PlatformUpdateRolloutStepAction = "node_healthy"
	PlatformUpdateRolloutStepSucceeded          PlatformUpdateRolloutStepAction = "rollout_succeeded"
	PlatformUpdateRolloutStepFailed             PlatformUpdateRolloutStepAction = "rollout_failed"
	PlatformUpdateRolloutStepOutcomeUnknown     PlatformUpdateRolloutStepAction = "outcome_unknown"
	PlatformUpdateRolloutStepNoChange           PlatformUpdateRolloutStepAction = "no_change"
)

type PlatformUpdateRolloutStepResult struct {
	RolloutID     string
	RolloutStatus PlatformUpdateRolloutStatus
	ServerID      string
	JobID         string
	Action        PlatformUpdateRolloutStepAction
	WaitingReason string
	ErrorCode     string
	BlockerCode   string
}

type platformUpdateRolloutStepState struct {
	RolloutStatus PlatformUpdateRolloutStatus
	ServerID      string
	JobID         string
	ErrorCode     string
	BlockerCode   string
}

// AdvancePlatformUpdateRollout composes the existing E3d admission and E3e
// health-reconciliation boundaries. One invocation performs at most one durable
// orchestration transition and never holds a controller-owned transaction or
// lock across either authoritative call.
func (r *Repository) AdvancePlatformUpdateRollout(ctx context.Context, rolloutID string) (PlatformUpdateRolloutStepResult, error) {
	canonicalRolloutID, err := canonicalPlatformUpdateServerID(rolloutID)
	if err != nil {
		return PlatformUpdateRolloutStepResult{}, fmt.Errorf("invalid rollout id: %w", err)
	}

	state, err := r.inspectPlatformUpdateRolloutStepState(ctx, canonicalRolloutID)
	if err != nil {
		return PlatformUpdateRolloutStepResult{}, err
	}
	if result, terminal := terminalPlatformUpdateRolloutStepResult(canonicalRolloutID, state); terminal {
		return result, nil
	}

	if state.RolloutStatus == PlatformUpdateRolloutRunning && state.JobID != "" {
		health, healthErr := r.ReconcilePlatformUpdateRolloutHealth(ctx, canonicalRolloutID)
		if healthErr != nil {
			// E3e infrastructure/commit errors are authoritative. Do not convert
			// commit uncertainty into a successful terminal observation merely
			// because another invocation terminalized the rollout concurrently.
			return PlatformUpdateRolloutStepResult{}, healthErr
		}

		result := PlatformUpdateRolloutStepResult{
			RolloutID:     canonicalRolloutID,
			RolloutStatus: PlatformUpdateRolloutStatus(health.RolloutStatus),
			ServerID:      health.ServerID,
			JobID:         health.JobID,
			WaitingReason: health.WaitingReason,
			ErrorCode:     health.ErrorCode,
			BlockerCode:   health.BlockerCode,
			Action:        platformUpdateRolloutStepActionFromHealth(health),
		}
		return result, nil
	}

	job, replayed, admissionErr := r.admitPlatformUpdateRolloutMutationWithDisposition(ctx, canonicalRolloutID)
	if admissionErr == nil {
		action := PlatformUpdateRolloutStepMutationAdmitted
		if replayed {
			action = PlatformUpdateRolloutStepMutationInProgress
		}
		return PlatformUpdateRolloutStepResult{
			RolloutID:     canonicalRolloutID,
			RolloutStatus: PlatformUpdateRolloutRunning,
			ServerID:      job.ServerID,
			JobID:         job.ID,
			Action:        action,
		}, nil
	}

	// Only explicit E3d domain outcomes are eligible for the single read-only
	// terminal-race normalization. Genuine PostgreSQL, transaction, commit, and
	// other infrastructure errors remain authoritative and are returned intact.
	if shouldNormalizePlatformUpdateRolloutAdmissionError(admissionErr) {
		if normalized, ok := r.normalizePlatformUpdateRolloutTerminalRace(ctx, canonicalRolloutID); ok {
			return normalized, nil
		}
	}
	return PlatformUpdateRolloutStepResult{}, admissionErr
}

func platformUpdateRolloutStepActionFromHealth(health PlatformUpdateRolloutHealthResult) PlatformUpdateRolloutStepAction {
	switch PlatformUpdateRolloutStatus(health.RolloutStatus) {
	case PlatformUpdateRolloutSucceeded:
		return PlatformUpdateRolloutStepSucceeded
	case PlatformUpdateRolloutFailed:
		return PlatformUpdateRolloutStepFailed
	case PlatformUpdateRolloutOutcomeUnknown:
		return PlatformUpdateRolloutStepOutcomeUnknown
	}

	switch PlatformUpdateRolloutEntryStatus(health.EntryStatus) {
	case PlatformUpdateRolloutEntryHealthy:
		return PlatformUpdateRolloutStepNodeHealthy
	case PlatformUpdateRolloutEntryFailed:
		return PlatformUpdateRolloutStepFailed
	case PlatformUpdateRolloutEntryOutcomeUnknown:
		return PlatformUpdateRolloutStepOutcomeUnknown
	case PlatformUpdateRolloutEntryUpdating:
		return PlatformUpdateRolloutStepWaitingHealth
	default:
		// Another invocation may have completed the entry after the controller's
		// initial inspection. E3e then authoritatively observes no updating entry
		// and returns a running rollout with no entry identity. That is not a
		// health wait; a later invocation may attempt the next E3d admission.
		return PlatformUpdateRolloutStepNoChange
	}
}

func shouldNormalizePlatformUpdateRolloutAdmissionError(err error) bool {
	return errors.Is(err, ErrPlatformUpdateRolloutComplete) ||
		errors.Is(err, ErrPlatformUpdateRolloutNotMutationRunnable) ||
		errors.Is(err, ErrPlatformUpdateRolloutAdmissionFailed)
}

func (r *Repository) normalizePlatformUpdateRolloutTerminalRace(ctx context.Context, rolloutID string) (PlatformUpdateRolloutStepResult, bool) {
	state, err := r.inspectPlatformUpdateRolloutStepState(ctx, rolloutID)
	if err != nil {
		return PlatformUpdateRolloutStepResult{}, false
	}
	return terminalPlatformUpdateRolloutStepResult(rolloutID, state)
}

func (r *Repository) inspectPlatformUpdateRolloutStepState(ctx context.Context, rolloutID string) (platformUpdateRolloutStepState, error) {
	var state platformUpdateRolloutStepState
	var rolloutStatus string
	if err := r.pool.QueryRow(ctx, `
		SELECT r.status,
		       COALESCE(r.error_code, ''),
		       COALESCE(e.server_id::text, ''),
		       COALESCE(e.platform_update_job_id::text, ''),
		       COALESCE(e.blocker_code, '')
		FROM platform_update_rollouts r
		LEFT JOIN LATERAL (
			SELECT server_id, platform_update_job_id, blocker_code
			FROM platform_update_rollout_entries
			WHERE rollout_id = r.id
			  AND status IN ('updating', 'failed', 'outcome_unknown')
			ORDER BY position
			LIMIT 1
		) e ON TRUE
		WHERE r.id = $1::uuid
	`, rolloutID).Scan(&rolloutStatus, &state.ErrorCode, &state.ServerID, &state.JobID, &state.BlockerCode); err != nil {
		return platformUpdateRolloutStepState{}, err
	}
	state.RolloutStatus = PlatformUpdateRolloutStatus(rolloutStatus)
	return state, nil
}

func terminalPlatformUpdateRolloutStepResult(rolloutID string, state platformUpdateRolloutStepState) (PlatformUpdateRolloutStepResult, bool) {
	result := PlatformUpdateRolloutStepResult{
		RolloutID:     rolloutID,
		RolloutStatus: state.RolloutStatus,
		ServerID:      state.ServerID,
		JobID:         state.JobID,
		ErrorCode:     state.ErrorCode,
		BlockerCode:   state.BlockerCode,
	}
	switch state.RolloutStatus {
	case PlatformUpdateRolloutSucceeded:
		result.Action = PlatformUpdateRolloutStepSucceeded
	case PlatformUpdateRolloutFailed:
		result.Action = PlatformUpdateRolloutStepFailed
	case PlatformUpdateRolloutOutcomeUnknown:
		result.Action = PlatformUpdateRolloutStepOutcomeUnknown
	default:
		return PlatformUpdateRolloutStepResult{}, false
	}
	return result, true
}
