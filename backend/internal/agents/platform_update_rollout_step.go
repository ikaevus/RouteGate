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
}

type platformUpdateRolloutStepState struct {
	RolloutStatus PlatformUpdateRolloutStatus
	ServerID      string
	JobID         string
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
			WaitingReason: health.WaitingReason,
		}

		// The pre-E3e inspection is advisory only. Another controller invocation
		// can advance the rollout before E3e obtains its authoritative locks. Only
		// report the inspected server/job identity when a post-reconciliation read
		// proves that the same entry is still the durable updating entry. Otherwise
		// omit identity rather than attributing E3e's result to a stale bound job.
		if confirmed, confirmErr := r.inspectPlatformUpdateRolloutStepState(ctx, canonicalRolloutID); confirmErr == nil &&
			confirmed.RolloutStatus == PlatformUpdateRolloutRunning &&
			confirmed.ServerID == state.ServerID && confirmed.JobID == state.JobID {
			result.ServerID = state.ServerID
			result.JobID = state.JobID
		}

		switch PlatformUpdateRolloutStatus(health.RolloutStatus) {
		case PlatformUpdateRolloutSucceeded:
			result.Action = PlatformUpdateRolloutStepSucceeded
			return result, nil
		case PlatformUpdateRolloutFailed:
			result.Action = PlatformUpdateRolloutStepFailed
			return result, nil
		case PlatformUpdateRolloutOutcomeUnknown:
			result.Action = PlatformUpdateRolloutStepOutcomeUnknown
			return result, nil
		}

		switch PlatformUpdateRolloutEntryStatus(health.EntryStatus) {
		case PlatformUpdateRolloutEntryHealthy:
			result.Action = PlatformUpdateRolloutStepNodeHealthy
			return result, nil
		case PlatformUpdateRolloutEntryFailed:
			result.Action = PlatformUpdateRolloutStepFailed
			return result, nil
		case PlatformUpdateRolloutEntryOutcomeUnknown:
			result.Action = PlatformUpdateRolloutStepOutcomeUnknown
			return result, nil
		default:
			// A successful E3e waiting result is authoritative. Do not perform
			// terminal-race normalization here; normalization is reserved for the
			// explicit E3d domain race outcomes below.
			result.Action = PlatformUpdateRolloutStepWaitingHealth
			return result, nil
		}
	}

	job, admissionErr := r.AdmitPlatformUpdateRolloutMutation(ctx, canonicalRolloutID)
	if admissionErr == nil {
		return PlatformUpdateRolloutStepResult{
			RolloutID:     canonicalRolloutID,
			RolloutStatus: PlatformUpdateRolloutRunning,
			ServerID:      job.ServerID,
			JobID:         job.ID,
			Action:        PlatformUpdateRolloutStepMutationAdmitted,
		}, nil
	}

	// Only explicit E3d domain outcomes are eligible for the single read-only
	// terminal-race normalization. Genuine PostgreSQL, transaction, commit, and
	// other infrastructure errors remain authoritative and are returned intact.
	if errors.Is(admissionErr, ErrPlatformUpdateRolloutComplete) ||
		errors.Is(admissionErr, ErrPlatformUpdateRolloutNotMutationRunnable) {
		if normalized, ok := r.normalizePlatformUpdateRolloutTerminalRace(ctx, canonicalRolloutID); ok {
			return normalized, nil
		}
	}
	return PlatformUpdateRolloutStepResult{}, admissionErr
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
		       COALESCE(e.server_id::text, ''),
		       COALESCE(e.platform_update_job_id::text, '')
		FROM platform_update_rollouts r
		LEFT JOIN LATERAL (
			SELECT server_id, platform_update_job_id
			FROM platform_update_rollout_entries
			WHERE rollout_id = r.id
			  AND status = 'updating'
			ORDER BY position
			LIMIT 1
		) e ON TRUE
		WHERE r.id = $1::uuid
	`, rolloutID).Scan(&rolloutStatus, &state.ServerID, &state.JobID); err != nil {
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
