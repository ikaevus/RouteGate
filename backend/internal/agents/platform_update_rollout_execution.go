package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const platformUpdateRolloutAdmissionRejected = "admission_rejected"

// AdmitPlatformUpdateRolloutMutation starts or resumes a durable rollout and
// atomically admits at most one single-node platform update. The caller selects
// only the durable rollout identity; server identity and target version are read
// from the immutable rollout snapshot.
func (r *Repository) AdmitPlatformUpdateRolloutMutation(ctx context.Context, rolloutID string) (PlatformUpdateJob, error) {
	canonicalRolloutID, err := canonicalPlatformUpdateServerID(rolloutID)
	if err != nil {
		return PlatformUpdateJob{}, fmt.Errorf("invalid rollout id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PlatformUpdateJob{}, err
	}
	defer tx.Rollback(ctx)

	var targetVersion, rolloutStatus string
	if err := tx.QueryRow(ctx, `
		SELECT target_version, status
		FROM platform_update_rollouts
		WHERE id = $1::uuid
		FOR UPDATE
	`, canonicalRolloutID).Scan(&targetVersion, &rolloutStatus); err != nil {
		return PlatformUpdateJob{}, err
	}

	switch rolloutStatus {
	case "pending":
		if _, err := tx.Exec(ctx, `
			UPDATE platform_update_rollouts
			SET status = 'running', started_at = now(), updated_at = now()
			WHERE id = $1::uuid AND status = 'pending'
		`, canonicalRolloutID); err != nil {
			return PlatformUpdateJob{}, err
		}
	case "running":
		// Resume from durable state below.
	default:
		return PlatformUpdateJob{}, fmt.Errorf("rollout is not mutation-runnable: %s", rolloutStatus)
	}

	// Replay/restart safety: a bound updating entry is authoritative. Return its
	// exact job rather than creating any replacement.
	job, err := scanPlatformUpdateJob(tx.QueryRow(ctx, `
		SELECT
			j.id::text,
			j.server_id::text,
			j.target_version,
			j.status,
			COALESCE(j.error_code, ''),
			j.created_at,
			j.updated_at,
			j.started_at,
			j.dispatched_at,
			j.completed_at
		FROM platform_update_rollout_entries e
		JOIN agent_platform_update_jobs j ON j.id = e.platform_update_job_id
		WHERE e.rollout_id = $1::uuid
		  AND e.status = 'updating'
		ORDER BY e.position
		LIMIT 1
	`, canonicalRolloutID))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return PlatformUpdateJob{}, err
		}
		return job, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PlatformUpdateJob{}, err
	}

	var blockedTerminal bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM platform_update_rollout_entries
			WHERE rollout_id = $1::uuid
			  AND status IN ('failed', 'outcome_unknown')
		)
	`, canonicalRolloutID).Scan(&blockedTerminal); err != nil {
		return PlatformUpdateJob{}, err
	}
	if blockedTerminal {
		return PlatformUpdateJob{}, fmt.Errorf("rollout contains a terminal stop entry")
	}

	var entryID, serverID string
	var position int
	if err := tx.QueryRow(ctx, `
		SELECT id::text, server_id::text, position
		FROM platform_update_rollout_entries e
		WHERE rollout_id = $1::uuid
		  AND status IN ('queued', 'waiting')
		  AND platform_update_job_id IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM platform_update_rollout_entries prior
			WHERE prior.rollout_id = e.rollout_id
			  AND prior.position < e.position
			  AND prior.status NOT IN ('healthy', 'skipped')
		  )
		ORDER BY position
		LIMIT 1
		FOR UPDATE
	`, canonicalRolloutID).Scan(&entryID, &serverID, &position); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformUpdateJob{}, fmt.Errorf("rollout has no admissible next entry")
		}
		return PlatformUpdateJob{}, err
	}

	canonicalServerID, err := canonicalPlatformUpdateServerID(serverID)
	if err != nil || canonicalServerID != serverID {
		return PlatformUpdateJob{}, fmt.Errorf("persisted rollout server identity is not canonical")
	}
	if !validPlatformUpdateTargetVersion(targetVersion) {
		return PlatformUpdateJob{}, fmt.Errorf("persisted rollout target version is invalid")
	}

	job, err = createPlatformUpdateJobTx(ctx, tx, CreatePlatformUpdateJobInput{
		ServerID:      serverID,
		TargetVersion: targetVersion,
	})
	if err != nil {
		if errors.Is(err, ErrPlatformUpdateAdmissionRejected) {
			if _, updateErr := tx.Exec(ctx, `
				UPDATE platform_update_rollouts
				SET status = 'failed', error_code = $2, completed_at = now(), updated_at = now()
				WHERE id = $1::uuid AND status = 'running'
			`, canonicalRolloutID, platformUpdateRolloutAdmissionRejected); updateErr != nil {
				return PlatformUpdateJob{}, updateErr
			}
			if err := tx.Commit(ctx); err != nil {
				return PlatformUpdateJob{}, err
			}
			return PlatformUpdateJob{}, ErrPlatformUpdateAdmissionRejected
		}
		return PlatformUpdateJob{}, err
	}

	commandTag, err := tx.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET status = 'updating', platform_update_job_id = $2::uuid, updated_at = now()
		WHERE id = $1::uuid
		  AND status IN ('queued', 'waiting')
		  AND platform_update_job_id IS NULL
	`, entryID, job.ID)
	if err != nil {
		return PlatformUpdateJob{}, err
	}
	if commandTag.RowsAffected() != 1 {
		return PlatformUpdateJob{}, fmt.Errorf("rollout entry admission lost durable ownership")
	}

	_ = position // retained in the locked selection for explicit persisted-order semantics.
	if err := tx.Commit(ctx); err != nil {
		return PlatformUpdateJob{}, err
	}
	return job, nil
}
