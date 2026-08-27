package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
	"github.com/jackc/pgx/v5"
)

// PersistPlatformUpdateRolloutPlan atomically records a pending rollout and its
// ordered eligibility snapshot. Caller-supplied Eligible/Blockers values are
// deliberately ignored: eligibility is re-derived from Manager-owned state
// while the relevant server and Agent rows are locked in this transaction.
// It does not start the rollout, create update jobs, or dispatch Agent work.
func (r *Repository) PersistPlatformUpdateRolloutPlan(ctx context.Context, plan PlatformUpdateRolloutPlan) (string, error) {
	if !validPlatformUpdateTargetVersion(plan.TargetVersion) {
		return "", fmt.Errorf("invalid target version")
	}
	if len(plan.Entries) == 0 {
		return "", fmt.Errorf("at least one rollout plan entry is required")
	}

	seen := make(map[string]struct{}, len(plan.Entries))
	serverIDs := make([]string, 0, len(plan.Entries))
	for _, entry := range plan.Entries {
		serverID, err := canonicalPlatformUpdateServerID(entry.ServerID)
		if err != nil || serverID != entry.ServerID {
			return "", fmt.Errorf("rollout plan contains non-canonical server id")
		}
		if _, ok := seen[serverID]; ok {
			return "", fmt.Errorf("duplicate rollout plan server id %q", serverID)
		}
		seen[serverID] = struct{}{}
		serverIDs = append(serverIDs, serverID)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	entries := make([]PlatformUpdateRolloutPlanEntry, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		entry, err := revalidatePlatformUpdateRolloutEntry(ctx, tx, serverID, plan.TargetVersion)
		if err != nil {
			return "", err
		}
		entries = append(entries, entry)
	}

	var rolloutID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO platform_update_rollouts (target_version)
		VALUES ($1)
		RETURNING id::text
	`, plan.TargetVersion).Scan(&rolloutID); err != nil {
		return "", err
	}

	for position, entry := range entries {
		blockers := make([]string, len(entry.Blockers))
		for i, blocker := range entry.Blockers {
			blockers[i] = string(blocker)
		}
		status := "queued"
		if entry.Eligible {
			_, err = tx.Exec(ctx, `
				INSERT INTO platform_update_rollout_entries
					(rollout_id, server_id, target_version, position, status, planning_blockers)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::text[])
			`, rolloutID, entry.ServerID, plan.TargetVersion, position, status, blockers)
		} else {
			status = "skipped"
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

func revalidatePlatformUpdateRolloutEntry(ctx context.Context, tx pgx.Tx, serverID, targetVersion string) (PlatformUpdateRolloutPlanEntry, error) {
	entry := PlatformUpdateRolloutPlanEntry{ServerID: serverID}
	if buildinfo.Current().Version != targetVersion {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerManagerVersionMismatch)
	}

	var deploymentRole, serverStatus string
	if err := tx.QueryRow(ctx, `
		SELECT deployment_role, status
		FROM servers
		WHERE id = $1::uuid
		FOR UPDATE
	`, serverID).Scan(&deploymentRole, &serverStatus); err != nil {
		return PlatformUpdateRolloutPlanEntry{}, err
	}
	if deploymentRole != "vpn" {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerNotVPNRole)
	}
	if serverStatus == "disabled" {
		entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerServerDisabled)
	}

	var agentStatus, agentVersion string
	var protocolVersion *int
	var capabilitiesJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT status, agent_version, protocol_version, capabilities
		FROM agents
		WHERE server_id = $1::uuid
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`, serverID).Scan(&agentStatus, &agentVersion, &protocolVersion, &capabilitiesJSON)
	if err == pgx.ErrNoRows {
		entry.Blockers = append(entry.Blockers,
			PlatformUpdateRolloutBlockerAgentMissing,
			PlatformUpdateRolloutBlockerUpdateCapability,
			PlatformUpdateRolloutBlockerProtocolIncompatible,
		)
	} else if err != nil {
		return PlatformUpdateRolloutPlanEntry{}, err
	} else {
		if agentStatus == StatusDisabled {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerAgentDisabled)
		}
		var capabilities Capabilities
		if err := json.Unmarshal(capabilitiesJSON, &capabilities); err != nil || !platformUpdateCapabilityReady(capabilities) {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerUpdateCapability)
		}
		compatibility := EvaluateCompatibility(agentVersion, protocolVersion)
		if compatibility.Status != CompatibilityCompatible && compatibility.Status != CompatibilityUpgradeRecommended {
			entry.Blockers = append(entry.Blockers, PlatformUpdateRolloutBlockerProtocolIncompatible)
		}
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

	entry.Eligible = len(entry.Blockers) == 0
	return entry, nil
}

func platformUpdateCapabilityReady(capabilities Capabilities) bool {
	encoded, err := json.Marshal(capabilities["softwareUpdate"])
	if err != nil {
		return false
	}
	var capability struct {
		SchemaVersion int    `json:"schemaVersion"`
		State         string `json:"state"`
		Request       string `json:"request"`
	}
	if err := json.Unmarshal(encoded, &capability); err != nil {
		return false
	}
	return capability.SchemaVersion == PlatformUpdateCapabilitySchemaVersion &&
		capability.State == PlatformUpdateCapabilityStateReady &&
		capability.Request == PlatformUpdateCapabilityRequestVersionOnly
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
