package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	platformUpdateRolloutHealthFreshness = 2 * time.Minute

	PlatformUpdateRolloutHealthWaitingJob         = "job_not_terminal"
	PlatformUpdateRolloutHealthWaitingHeartbeat   = "heartbeat_not_proven"
	PlatformUpdateRolloutHealthWaitingAgent       = "agent_not_currently_healthy"
	PlatformUpdateRolloutHealthInterveningHistory = "intervening_update_history"
	PlatformUpdateRolloutHealthNodeFailed         = "node_update_failed"
	PlatformUpdateRolloutHealthOutcomeUnknown     = "node_update_outcome_unknown"
)

type PlatformUpdateRolloutHealthResult struct {
	RolloutStatus string
	EntryStatus   string
	WaitingReason string
	ServerID      string
	JobID         string
}

// ReconcilePlatformUpdateRolloutHealth converts the one currently updating
// rollout entry into a terminal result only from durable post-update proof. It
// never creates a platform-update job; admitting the next node remains a
// separate E3d transaction after this proof commits.
func (r *Repository) ReconcilePlatformUpdateRolloutHealth(ctx context.Context, rolloutID string) (PlatformUpdateRolloutHealthResult, error) {
	canonicalRolloutID, err := canonicalPlatformUpdateServerID(rolloutID)
	if err != nil {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("invalid rollout id: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT lock_platform_update_admission_global()`); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}

	var targetVersion, rolloutStatus string
	if err := tx.QueryRow(ctx, `
		SELECT target_version, status
		FROM platform_update_rollouts
		WHERE id = $1::uuid
		FOR UPDATE
	`, canonicalRolloutID).Scan(&targetVersion, &rolloutStatus); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}

	if rolloutStatus != "running" {
		if err := tx.Commit(ctx); err != nil {
			return PlatformUpdateRolloutHealthResult{}, err
		}
		return PlatformUpdateRolloutHealthResult{RolloutStatus: rolloutStatus}, nil
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('routegate.platform_update_admission_rollout_id', $1, true)`, canonicalRolloutID); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}

	var entryID, serverID, jobID string
	var observedUpdateJobCount *int64
	if err := tx.QueryRow(ctx, `
		SELECT id::text, server_id::text, platform_update_job_id::text, observed_update_job_count
		FROM platform_update_rollout_entries
		WHERE rollout_id = $1::uuid
		  AND status = 'updating'
		ORDER BY position
		LIMIT 1
		FOR UPDATE
	`, canonicalRolloutID).Scan(&entryID, &serverID, &jobID, &observedUpdateJobCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if err := completePlatformUpdateRolloutIfFinishedTx(ctx, tx, canonicalRolloutID); err != nil {
				return PlatformUpdateRolloutHealthResult{}, err
			}
			var currentStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM platform_update_rollouts WHERE id = $1::uuid`, canonicalRolloutID).Scan(&currentStatus); err != nil {
				return PlatformUpdateRolloutHealthResult{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return PlatformUpdateRolloutHealthResult{}, err
			}
			return PlatformUpdateRolloutHealthResult{RolloutStatus: currentStatus}, nil
		}
		return PlatformUpdateRolloutHealthResult{}, err
	}

	canonicalServerID, err := canonicalPlatformUpdateServerID(serverID)
	if err != nil || canonicalServerID != serverID {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("persisted rollout server identity is not canonical")
	}
	if err := lockPlatformUpdateServer(ctx, tx, serverID); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}

	// Read only immutable job identity first so we can lock the exact Agent row.
	// The full job lifecycle is re-read with a row lock after the Agent lock.
	var boundAgentID string
	if err := tx.QueryRow(ctx, `
		SELECT agent_id::text
		FROM agent_platform_update_jobs
		WHERE id = $1::uuid
		  AND server_id = $2::uuid
		  AND target_version = $3
	`, jobID, serverID, targetVersion).Scan(&boundAgentID); err != nil {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("read bound rollout job identity: %w", err)
	}

	var agentStatus, agentVersion string
	var protocolVersion sql.NullInt64
	var credentialGeneration int64
	var heartbeatAt sql.NullTime
	var heartbeatGeneration sql.NullInt64
	if err := tx.QueryRow(ctx, `
		SELECT status,
		       agent_version,
		       protocol_version,
		       credential_generation,
		       last_authenticated_heartbeat_at,
		       last_authenticated_heartbeat_generation
		FROM agents
		WHERE id = $1::uuid
		  AND server_id = $2::uuid
		FOR UPDATE
	`, boundAgentID, serverID).Scan(
		&agentStatus,
		&agentVersion,
		&protocolVersion,
		&credentialGeneration,
		&heartbeatAt,
		&heartbeatGeneration,
	); err != nil {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("lock current rollout Agent: %w", err)
	}

	var jobStatus string
	var jobCompletedAt sql.NullTime
	if err := tx.QueryRow(ctx, `
		SELECT status, completed_at
		FROM agent_platform_update_jobs
		WHERE id = $1::uuid
		  AND server_id = $2::uuid
		  AND agent_id = $3::uuid
		  AND target_version = $4
		FOR UPDATE
	`, jobID, serverID, boundAgentID, targetVersion).Scan(&jobStatus, &jobCompletedAt); err != nil {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("lock bound rollout job: %w", err)
	}

	var proofNow time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&proofNow); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}

	switch jobStatus {
	case "pending", "in_progress", "mutation_dispatched":
		return commitPlatformUpdateRolloutHealthWait(ctx, tx, PlatformUpdateRolloutHealthResult{
			RolloutStatus: "running", EntryStatus: "updating", WaitingReason: PlatformUpdateRolloutHealthWaitingJob,
			ServerID: serverID, JobID: jobID,
		})
	case "failed":
		return terminalizePlatformUpdateRolloutHealthTx(ctx, tx, canonicalRolloutID, entryID, serverID, jobID, "failed", PlatformUpdateRolloutHealthNodeFailed)
	case "outcome_unknown":
		return terminalizePlatformUpdateRolloutHealthTx(ctx, tx, canonicalRolloutID, entryID, serverID, jobID, "outcome_unknown", PlatformUpdateRolloutHealthOutcomeUnknown)
	case "succeeded":
		if !jobCompletedAt.Valid {
			return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("succeeded rollout job has no completion timestamp")
		}
	default:
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("unsupported rollout job status %q", jobStatus)
	}

	var currentUpdateJobCount int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_platform_update_jobs
		WHERE server_id = $1::uuid
	`, serverID).Scan(&currentUpdateJobCount); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	if observedUpdateJobCount == nil || currentUpdateJobCount != *observedUpdateJobCount+1 {
		return terminalizePlatformUpdateRolloutHealthTx(ctx, tx, canonicalRolloutID, entryID, serverID, jobID, "failed", PlatformUpdateRolloutHealthInterveningHistory)
	}

	var unresolved bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_platform_update_jobs
			WHERE server_id = $1::uuid
			  AND status IN ('pending', 'in_progress', 'mutation_dispatched', 'outcome_unknown')
		)
	`, serverID).Scan(&unresolved); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	if unresolved {
		return commitPlatformUpdateRolloutHealthWait(ctx, tx, PlatformUpdateRolloutHealthResult{
			RolloutStatus: "running", EntryStatus: "updating", WaitingReason: PlatformUpdateRolloutHealthWaitingJob,
			ServerID: serverID, JobID: jobID,
		})
	}

	if !heartbeatAt.Valid || !heartbeatGeneration.Valid || heartbeatGeneration.Int64 != credentialGeneration ||
		!heartbeatAt.Time.After(jobCompletedAt.Time) || heartbeatAt.Time.After(proofNow) ||
		proofNow.Sub(heartbeatAt.Time) > platformUpdateRolloutHealthFreshness {
		return commitPlatformUpdateRolloutHealthWait(ctx, tx, PlatformUpdateRolloutHealthResult{
			RolloutStatus: "running", EntryStatus: "updating", WaitingReason: PlatformUpdateRolloutHealthWaitingHeartbeat,
			ServerID: serverID, JobID: jobID,
		})
	}

	var protocolPtr *int
	if protocolVersion.Valid {
		value := int(protocolVersion.Int64)
		protocolPtr = &value
	}
	compatibility := EvaluateCompatibility(agentVersion, protocolPtr)
	if agentStatus != StatusOnline || agentVersion != targetVersion ||
		(compatibility.Status != CompatibilityCompatible && compatibility.Status != CompatibilityUpgradeRecommended) {
		return commitPlatformUpdateRolloutHealthWait(ctx, tx, PlatformUpdateRolloutHealthResult{
			RolloutStatus: "running", EntryStatus: "updating", WaitingReason: PlatformUpdateRolloutHealthWaitingAgent,
			ServerID: serverID, JobID: jobID,
		})
	}

	commandTag, err := tx.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET status = 'healthy',
		    blocker_code = NULL,
		    completed_at = clock_timestamp(),
		    updated_at = clock_timestamp()
		WHERE id = $1::uuid
		  AND rollout_id = $2::uuid
		  AND server_id = $3::uuid
		  AND platform_update_job_id = $4::uuid
		  AND status = 'updating'
	`, entryID, canonicalRolloutID, serverID, jobID)
	if err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	if commandTag.RowsAffected() != 1 {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("rollout health proof lost durable ownership")
	}

	if err := completePlatformUpdateRolloutIfFinishedTx(ctx, tx, canonicalRolloutID); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	var finalRolloutStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM platform_update_rollouts WHERE id = $1::uuid`, canonicalRolloutID).Scan(&finalRolloutStatus); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	return PlatformUpdateRolloutHealthResult{RolloutStatus: finalRolloutStatus, EntryStatus: "healthy", ServerID: serverID, JobID: jobID}, nil
}

func commitPlatformUpdateRolloutHealthWait(ctx context.Context, tx pgx.Tx, result PlatformUpdateRolloutHealthResult) (PlatformUpdateRolloutHealthResult, error) {
	if err := tx.Commit(ctx); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	return result, nil
}

func terminalizePlatformUpdateRolloutHealthTx(ctx context.Context, tx pgx.Tx, rolloutID, entryID, serverID, jobID, status, errorCode string) (PlatformUpdateRolloutHealthResult, error) {
	entryTag, err := tx.Exec(ctx, `
		UPDATE platform_update_rollout_entries
		SET status = $2,
		    blocker_code = $3,
		    completed_at = clock_timestamp(),
		    updated_at = clock_timestamp()
		WHERE id = $1::uuid
		  AND status = 'updating'
	`, entryID, status, errorCode)
	if err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	if entryTag.RowsAffected() != 1 {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("rollout terminal health transition lost durable ownership")
	}

	rolloutTag, err := tx.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET status = $2,
		    error_code = $3,
		    completed_at = clock_timestamp(),
		    updated_at = clock_timestamp()
		WHERE id = $1::uuid
		  AND status = 'running'
	`, rolloutID, status, errorCode)
	if err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	if rolloutTag.RowsAffected() != 1 {
		return PlatformUpdateRolloutHealthResult{}, fmt.Errorf("rollout terminalization lost durable ownership")
	}
	if err := tx.Commit(ctx); err != nil {
		return PlatformUpdateRolloutHealthResult{}, err
	}
	return PlatformUpdateRolloutHealthResult{RolloutStatus: status, EntryStatus: status, ServerID: serverID, JobID: jobID}, nil
}

func completePlatformUpdateRolloutIfFinishedTx(ctx context.Context, tx pgx.Tx, rolloutID string) error {
	var unfinished bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM platform_update_rollout_entries
			WHERE rollout_id = $1::uuid
			  AND status NOT IN ('healthy', 'skipped')
		)
	`, rolloutID).Scan(&unfinished); err != nil {
		return err
	}
	if unfinished {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE platform_update_rollouts
		SET status = 'succeeded',
		    error_code = NULL,
		    completed_at = clock_timestamp(),
		    updated_at = clock_timestamp()
		WHERE id = $1::uuid
		  AND status = 'running'
	`, rolloutID)
	return err
}
