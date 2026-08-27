package agents

import (
	"context"
	"fmt"
)

// PersistPlatformUpdateRolloutPlan atomically records a pending rollout and its
// ordered eligibility snapshot. It does not start the rollout, create update
// jobs, or dispatch Agent work.
func (r *Repository) PersistPlatformUpdateRolloutPlan(ctx context.Context, plan PlatformUpdateRolloutPlan) (string, error) {
	if !validPlatformUpdateTargetVersion(plan.TargetVersion) {
		return "", fmt.Errorf("invalid target version")
	}
	if len(plan.Entries) == 0 {
		return "", fmt.Errorf("at least one rollout plan entry is required")
	}

	seen := make(map[string]struct{}, len(plan.Entries))
	for _, entry := range plan.Entries {
		serverID, err := canonicalPlatformUpdateServerID(entry.ServerID)
		if err != nil || serverID != entry.ServerID {
			return "", fmt.Errorf("rollout plan contains non-canonical server id")
		}
		if _, ok := seen[serverID]; ok {
			return "", fmt.Errorf("duplicate rollout plan server id %q", serverID)
		}
		seen[serverID] = struct{}{}
		if entry.Eligible != (len(entry.Blockers) == 0) {
			return "", fmt.Errorf("rollout plan eligibility evidence is inconsistent")
		}
		if len(entry.Blockers) > 8 {
			return "", fmt.Errorf("too many rollout planning blockers")
		}
		for _, blocker := range entry.Blockers {
			if !validPlatformUpdateRolloutBlocker(blocker) {
				return "", fmt.Errorf("invalid rollout planning blocker")
			}
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var rolloutID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO platform_update_rollouts (target_version)
		VALUES ($1)
		RETURNING id::text
	`, plan.TargetVersion).Scan(&rolloutID); err != nil {
		return "", err
	}

	for position, entry := range plan.Entries {
		blockers := make([]string, len(entry.Blockers))
		for i, blocker := range entry.Blockers {
			blockers[i] = string(blocker)
		}
		status := "queued"
		var completedAt any
		if !entry.Eligible {
			status = "skipped"
			completedAt = "now"
		}
		if completedAt == nil {
			_, err = tx.Exec(ctx, `
				INSERT INTO platform_update_rollout_entries
					(rollout_id, server_id, target_version, position, status, planning_blockers)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::text[])
			`, rolloutID, entry.ServerID, plan.TargetVersion, position, status, blockers)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO platform_update_rollout_entries
					(rollout_id, server_id, target_version, position, status, planning_blockers, completed_at)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::text[], now())
			`, rolloutID, entry.ServerID, plan.TargetVersion, position, status, blockers)
		}
		if err != nil {
			return "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return rolloutID, nil
}

func validPlatformUpdateRolloutBlocker(blocker PlatformUpdateRolloutBlocker) bool {
	switch blocker {
	case PlatformUpdateRolloutBlockerManagerVersionMismatch,
		PlatformUpdateRolloutBlockerNotVPNRole,
		PlatformUpdateRolloutBlockerServerDisabled,
		PlatformUpdateRolloutBlockerAgentMissing,
		PlatformUpdateRolloutBlockerAgentDisabled,
		PlatformUpdateRolloutBlockerUpdateCapability,
		PlatformUpdateRolloutBlockerActiveUpdate,
		PlatformUpdateRolloutBlockerProtocolIncompatible:
		return true
	default:
		return false
	}
}
