package nodegroups

import (
	"context"
	"strings"
	"time"
)

// CandidatesForProtocol evaluates the group using one required protocol rather
// than each node's configured default. Automatic Selection uses this for VPN
// accounts with an explicit client-protocol preference. An empty/auto protocol
// preserves the normal per-node-default behavior.
func (r *Repository) CandidatesForProtocol(ctx context.Context, groupID, protocol string, now time.Time) (ListNodeGroupCandidatesResponse, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "auto" {
		return r.Candidates(ctx, groupID, now)
	}

	group, err := r.Get(ctx, groupID)
	if err != nil {
		return ListNodeGroupCandidatesResponse{}, err
	}

	rows, err := r.queries.Query(ctx, `
		SELECT
			s.id::text,
			s.name,
			$2::text,
			m.priority,
			m.weight,
			m.enabled,
			s.status,
			s.deployment_role,
			COALESCE(a.status, ''),
			a.last_seen_at,
			COALESCE(a.capabilities, '{}'::jsonb),
			a.runtime_load_1,
			a.runtime_logical_cpus
		FROM node_group_members m
		JOIN servers s ON s.id = m.server_id
		LEFT JOIN agents a ON a.server_id = s.id
		WHERE m.node_group_id = $1::uuid
		ORDER BY m.priority, lower(s.name), s.id
	`, groupID, protocol)
	if err != nil {
		return ListNodeGroupCandidatesResponse{}, err
	}
	defer rows.Close()

	response := ListNodeGroupCandidatesResponse{
		NodeGroupID:       group.ID,
		SelectionStrategy: group.SelectionStrategy,
		Candidates:        make([]NodeGroupCandidate, 0, group.MemberCount),
	}
	for rows.Next() {
		var row candidateRow
		if err := rows.Scan(
			&row.candidate.ServerID,
			&row.candidate.ServerName,
			&row.candidate.Protocol,
			&row.candidate.Priority,
			&row.candidate.Weight,
			&row.candidate.MemberEnabled,
			&row.candidate.NodeStatus,
			&row.deploymentRole,
			&row.candidate.AgentStatus,
			&row.lastSeen,
			&row.capabilities,
			&row.load1,
			&row.logicalCPUs,
		); err != nil {
			return ListNodeGroupCandidatesResponse{}, err
		}
		if row.load1.Valid {
			value := row.load1.Float64
			row.candidate.Load1 = &value
		}
		if row.lastSeen.Valid {
			value := row.lastSeen.Time
			row.candidate.LastSeenAt = &value
		}
		if row.logicalCPUs.Valid {
			value := int(row.logicalCPUs.Int32)
			row.candidate.LogicalCPUs = &value
		}
		row.candidate.ProtocolSupported, row.candidate.RuntimeState = capabilityEvidence(row.capabilities, protocol)
		row.candidate = evaluateCandidate(row.candidate, row.deploymentRole, now)
		response.Candidates = append(response.Candidates, row.candidate)
	}
	return response, rows.Err()
}
