package agents

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/jackc/pgx/v5"
)

// PersistPlatformUpdateRolloutPlan atomically records a pending rollout and its
// ordered eligibility snapshot. Caller-supplied Eligible/Blockers values are
// deliberately ignored: eligibility is re-derived from Manager-owned state
// while the relevant update-admission lock and database rows are held in this
// transaction. It does not start the rollout, create update jobs, or dispatch
// Agent work.
func (r *Repository) PersistPlatformUpdateRolloutPlan(ctx context.Context, plan PlatformUpdateRolloutPlan) (string, error) {
	id, _, err := r.persistPlatformUpdateRolloutPlan(ctx, plan, "", "")
	return id, err
}

var ErrPlatformUpdateRolloutIdempotencyConflict = fmt.Errorf("platform update rollout idempotency conflict")

// PersistPlatformUpdateRolloutPlanIdempotent atomically binds creation evidence
// to the immutable E3c snapshot. Identical concurrent requests converge through
// the database uniqueness constraint; conflicting key reuse fails closed.
func (r *Repository) PersistPlatformUpdateRolloutPlanIdempotent(ctx context.Context, plan PlatformUpdateRolloutPlan, key, requestHash string) (string, bool, error) {
	return r.persistPlatformUpdateRolloutPlan(ctx, plan, key, requestHash)
}

func (r *Repository) persistPlatformUpdateRolloutPlan(ctx context.Context, plan PlatformUpdateRolloutPlan, key, requestHash string) (string, bool, error) {
	if !validPlatformUpdateTargetVersion(plan.TargetVersion) {
		return "", false, fmt.Errorf("invalid target version")
	}
	if len(plan.Entries) == 0 {
		return "", false, fmt.Errorf("at least one rollout plan entry is required")
	}

	seen := make(map[string]struct{}, len(plan.Entries))
	serverIDs := make([]string, 0, len(plan.Entries))
	for _, entry := range plan.Entries {
		serverID, err := canonicalPlatformUpdateServerID(entry.ServerID)
		if err != nil || serverID != entry.ServerID {
			return "", false, fmt.Errorf("rollout plan contains non-canonical server id")
		}
		if _, ok := seen[serverID]; ok {
			return "", false, fmt.Errorf("duplicate rollout plan server id %q", serverID)
		}
		seen[serverID] = struct{}{}
		serverIDs = append(serverIDs, serverID)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	// Acquire all per-server update-admission locks in deterministic order so
	// concurrent overlapping rollout snapshots cannot deadlock each other. The
	// original entry order remains unchanged for the persisted snapshot.
	lockIDs := append([]string(nil), serverIDs...)
	sort.Strings(lockIDs)
	for _, serverID := range lockIDs {
		if err := lockPlatformUpdateServer(ctx, tx, serverID); err != nil {
			return "", false, err
		}
	}

	entries := make([]PlatformUpdateRolloutPlanEntry, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		entry, err := revalidatePlatformUpdateRolloutEntry(ctx, tx, serverID, plan.TargetVersion)
		if err != nil {
			return "", false, err
		}
		entries = append(entries, entry)
	}

	var rolloutID string
	insertSQL := `INSERT INTO platform_update_rollouts (target_version) VALUES ($1) RETURNING id::text`
	args := []any{plan.TargetVersion}
	if key != "" {
		insertSQL = `INSERT INTO platform_update_rollouts (target_version, creation_idempotency_key, creation_request_hash) VALUES ($1, $2::uuid, $3) RETURNING id::text`
		args = append(args, key, requestHash)
	}
	if err := tx.QueryRow(ctx, insertSQL, args...).Scan(&rolloutID); err != nil {
		var pgErr *pgconn.PgError
		if key != "" && errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "platform_update_rollouts_creation_idempotency_key_key" {
			_ = tx.Rollback(ctx)
			var existingID, existingHash string
			if readErr := r.pool.QueryRow(ctx, `SELECT id::text, creation_request_hash FROM platform_update_rollouts WHERE creation_idempotency_key = $1::uuid`, key).Scan(&existingID, &existingHash); readErr != nil {
				return "", false, readErr
			}
			if existingHash != requestHash {
				return "", false, ErrPlatformUpdateRolloutIdempotencyConflict
			}
			return existingID, true, nil
		}
		return "", false, err
	}

	// Migration 142 enforces canonical server lock ordering for every rollout-entry
	// INSERT at the database boundary. Persist rows in that lock order while
	// carrying the caller's original position explicitly so rollout semantics are
	// unchanged for plans whose requested order differs from UUID sort order.
	type positionedEntry struct {
		position int
		entry    PlatformUpdateRolloutPlanEntry
	}
	persistedEntries := make([]positionedEntry, 0, len(entries))
	for position, entry := range entries {
		persistedEntries = append(persistedEntries, positionedEntry{position: position, entry: entry})
	}
	sort.Slice(persistedEntries, func(i, j int) bool {
		return persistedEntries[i].entry.ServerID < persistedEntries[j].entry.ServerID
	})

	for _, positioned := range persistedEntries {
		position := positioned.position
		entry := positioned.entry
		blockers := make([]string, len(entry.Blockers))
		for i, blocker := range entry.Blockers {
			blockers[i] = string(blocker)
		}
		status := "queued"
		if entry.Eligible {
			_, err = tx.Exec(ctx, `
				INSERT INTO platform_update_rollout_entries
					(rollout_id, server_id, target_version, position, status, planning_blockers, observed_update_job_count)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::text[], $7)
			`, rolloutID, entry.ServerID, plan.TargetVersion, position, status, blockers, entry.observedUpdateJobCount)
		} else {
			status = "skipped"
			_, err = tx.Exec(ctx, `
				INSERT INTO platform_update_rollout_entries
					(rollout_id, server_id, target_version, position, status, planning_blockers, observed_update_job_count, completed_at)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::text[], $7, now())
			`, rolloutID, entry.ServerID, plan.TargetVersion, position, status, blockers, entry.observedUpdateJobCount)
		}
		if err != nil {
			return "", false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return rolloutID, false, nil
}

func revalidatePlatformUpdateRolloutEntry(ctx context.Context, tx pgx.Tx, serverID, targetVersion string) (PlatformUpdateRolloutPlanEntry, error) {
	entry := PlatformUpdateRolloutPlanEntry{ServerID: serverID}
	if buildinfo.Current().Version != targetVersion {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerManagerVersionMismatch)
	}

	capabilityJSON, err := platformUpdateCapabilityJSON()
	if err != nil {
		return PlatformUpdateRolloutPlanEntry{}, err
	}

	var agentStatus, agentVersion string
	var protocolVersion *int
	var exactUpdateCapability bool
	readAgentForUpdate := func() error {
		return tx.QueryRow(ctx, `
			SELECT status, agent_version, protocol_version,
			       COALESCE(capabilities -> 'softwareUpdate' = $2::jsonb, false)
			FROM agents
			WHERE server_id = $1::uuid
			ORDER BY updated_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`, serverID, capabilityJSON).Scan(&agentStatus, &agentVersion, &protocolVersion, &exactUpdateCapability)
	}
	readAgentVisible := func() error {
		return tx.QueryRow(ctx, `
			SELECT status, agent_version, protocol_version,
			       COALESCE(capabilities -> 'softwareUpdate' = $2::jsonb, false)
			FROM agents
			WHERE server_id = $1::uuid
			ORDER BY updated_at DESC, id DESC
			LIMIT 1
		`, serverID, capabilityJSON).Scan(&agentStatus, &agentVersion, &protocolVersion, &exactUpdateCapability)
	}
	lockServer := func() (string, string, error) {
		var deploymentRole, serverStatus string
		err := tx.QueryRow(ctx, `
			SELECT deployment_role, status
			FROM servers
			WHERE id = $1::uuid
			FOR UPDATE
		`, serverID).Scan(&deploymentRole, &serverStatus)
		return deploymentRole, serverStatus, err
	}

	agentFound := true
	var deploymentRole, serverStatus string
	if err := readAgentForUpdate(); err == pgx.ErrNoRows {
		agentFound = false
		if _, err := tx.Exec(ctx, `SAVEPOINT rg96e3c_agent_absence`); err != nil {
			return PlatformUpdateRolloutPlanEntry{}, err
		}
		deploymentRole, serverStatus, err = lockServer()
		if err != nil {
			return PlatformUpdateRolloutPlanEntry{}, err
		}

		if err := readAgentVisible(); err == pgx.ErrNoRows {
			entry.Blockers = append(entry.Blockers,
				PlatformUpdateRolloutBlockerAgentMissing,
				PlatformUpdateRolloutBlockerUpdateCapability,
				PlatformUpdateRolloutBlockerProtocolIncompatible,
			)
			if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT rg96e3c_agent_absence`); err != nil {
				return PlatformUpdateRolloutPlanEntry{}, err
			}
		} else if err != nil {
			return PlatformUpdateRolloutPlanEntry{}, err
		} else {
			if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT rg96e3c_agent_absence`); err != nil {
				return PlatformUpdateRolloutPlanEntry{}, err
			}
			if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT rg96e3c_agent_absence`); err != nil {
				return PlatformUpdateRolloutPlanEntry{}, err
			}
			if err := readAgentForUpdate(); err != nil {
				return PlatformUpdateRolloutPlanEntry{}, err
			}
			agentFound = true
			deploymentRole, serverStatus, err = lockServer()
			if err != nil {
				return PlatformUpdateRolloutPlanEntry{}, err
			}
		}
	} else if err != nil {
		return PlatformUpdateRolloutPlanEntry{}, err
	} else {
		deploymentRole, serverStatus, err = lockServer()
		if err != nil {
			return PlatformUpdateRolloutPlanEntry{}, err
		}
	}

	if agentFound {
		if agentStatus == StatusDisabled {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerAgentDisabled)
		}
		if !exactUpdateCapability {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerUpdateCapability)
		}
		compatibility := EvaluateCompatibility(agentVersion, protocolVersion)
		if compatibility.Status != CompatibilityCompatible && compatibility.Status != CompatibilityUpgradeRecommended {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerProtocolIncompatible)
		}
	}

	if deploymentRole != "vpn" {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerNotVPNRole)
	}
	if serverStatus == "disabled" {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerServerDisabled)
	}

	var hasInterlock bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_platform_update_jobs
			WHERE server_id = $1::uuid
			  AND status IN ('pending', 'in_progress', 'mutation_dispatched', 'outcome_unknown')
		)
	`, serverID).Scan(&hasInterlock); err != nil {
		return PlatformUpdateRolloutPlanEntry{}, err
	}
	if hasInterlock {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerActiveUpdate)
	}

	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_platform_update_jobs
		WHERE server_id = $1::uuid
	`, serverID).Scan(&entry.observedUpdateJobCount); err != nil {
		return PlatformUpdateRolloutPlanEntry{}, err
	}

	entry.Eligible = len(entry.Blockers) == 0
	return entry, nil
}
