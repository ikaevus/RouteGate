package agents

import (
	"context"
	"fmt"
	"time"
)

const maxPlatformUpdateRolloutMembers = 1024

type PlatformUpdateRolloutView struct {
	ID            string                           `json:"id"`
	TargetVersion string                           `json:"targetVersion"`
	Status        PlatformUpdateRolloutStatus      `json:"status"`
	CreatedAt     time.Time                        `json:"createdAt"`
	StartedAt     *time.Time                       `json:"startedAt,omitempty"`
	CompletedAt   *time.Time                       `json:"completedAt,omitempty"`
	Entries       []PlatformUpdateRolloutEntryView `json:"entries"`
}

type PlatformUpdateRolloutEntryView struct {
	ServerID         string                           `json:"serverId"`
	Position         int                              `json:"position"`
	Status           PlatformUpdateRolloutEntryStatus `json:"status"`
	PlanningBlockers []PlatformUpdateRolloutBlocker   `json:"planningBlockers"`
	JobID            string                           `json:"jobId,omitempty"`
	CompletedAt      *time.Time                       `json:"completedAt,omitempty"`
}

// GetPlatformUpdateRollout returns only bounded Manager-owned durable state.
func (r *Repository) GetPlatformUpdateRollout(ctx context.Context, rolloutID string) (PlatformUpdateRolloutView, error) {
	canonicalID, err := canonicalPlatformUpdateServerID(rolloutID)
	if err != nil || canonicalID != rolloutID {
		return PlatformUpdateRolloutView{}, fmt.Errorf("invalid rollout id")
	}
	var view PlatformUpdateRolloutView
	var status string
	if err := r.pool.QueryRow(ctx, `
		SELECT id::text, target_version, status, created_at, started_at, completed_at
		FROM platform_update_rollouts WHERE id = $1::uuid
	`, canonicalID).Scan(&view.ID, &view.TargetVersion, &status, &view.CreatedAt, &view.StartedAt, &view.CompletedAt); err != nil {
		return PlatformUpdateRolloutView{}, err
	}
	view.Status = PlatformUpdateRolloutStatus(status)
	rows, err := r.pool.Query(ctx, `
		SELECT server_id::text, position, status, planning_blockers,
		       COALESCE(platform_update_job_id::text, ''), completed_at
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid ORDER BY position LIMIT $2
	`, canonicalID, maxPlatformUpdateRolloutMembers+1)
	if err != nil {
		return PlatformUpdateRolloutView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(view.Entries) == maxPlatformUpdateRolloutMembers {
			return PlatformUpdateRolloutView{}, fmt.Errorf("rollout membership exceeds response bound")
		}
		var entry PlatformUpdateRolloutEntryView
		var entryStatus string
		var blockers []string
		if err := rows.Scan(&entry.ServerID, &entry.Position, &entryStatus, &blockers, &entry.JobID, &entry.CompletedAt); err != nil {
			return PlatformUpdateRolloutView{}, err
		}
		entry.Status = PlatformUpdateRolloutEntryStatus(entryStatus)
		entry.PlanningBlockers = make([]PlatformUpdateRolloutBlocker, len(blockers))
		for i, blocker := range blockers {
			entry.PlanningBlockers[i] = PlatformUpdateRolloutBlocker(blocker)
		}
		view.Entries = append(view.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return PlatformUpdateRolloutView{}, err
	}
	return view, nil
}
