package nodegroups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNodeGroupNameExists = errors.New("node group name already exists")
	ErrNodeGroupAssigned   = errors.New("node group is assigned to one or more VPN accounts")
	ErrServerNotVPNNode    = errors.New("server does not host the VPN plane")
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const nodeGroupSelect = `
	SELECT
		g.id::text,
		g.name,
		COALESCE(g.description, ''),
		g.selection_strategy,
		COUNT(m.server_id)::int,
		g.created_at,
		g.updated_at
	FROM node_groups g
	LEFT JOIN node_group_members m ON m.node_group_id = g.id`

func (r *Repository) List(ctx context.Context) ([]NodeGroup, error) {
	rows, err := r.pool.Query(ctx, nodeGroupSelect+`
		GROUP BY g.id
		ORDER BY lower(g.name), g.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]NodeGroup, 0)
	for rows.Next() {
		group, scanErr := scanNodeGroup(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (NodeGroup, error) {
	group, err := scanNodeGroup(r.pool.QueryRow(ctx, nodeGroupSelect+`
		WHERE g.id = $1::uuid
		GROUP BY g.id
	`, id))
	if err != nil {
		return NodeGroup{}, err
	}
	members, err := r.ListMembers(ctx, id)
	if err != nil {
		return NodeGroup{}, err
	}
	group.Members = members
	return group, nil
}

func (r *Repository) Create(ctx context.Context, input CreateNodeGroupInput) (NodeGroup, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO node_groups (name, description, selection_strategy)
		VALUES ($1, NULLIF($2, ''), $3)
		RETURNING id::text
	`, input.Name, input.Description, input.SelectionStrategy).Scan(&id)
	if err != nil {
		return NodeGroup{}, mapWriteError(err)
	}
	return r.Get(ctx, id)
}

func (r *Repository) Update(ctx context.Context, id string, input UpdateNodeGroupInput) (NodeGroup, error) {
	var updatedID string
	err := r.pool.QueryRow(ctx, `
		UPDATE node_groups
		SET
			name = CASE WHEN $2 THEN $3 ELSE name END,
			description = CASE WHEN $4 THEN NULLIF($5, '') ELSE description END,
			selection_strategy = CASE WHEN $6 THEN $7 ELSE selection_strategy END,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text
	`, id,
		input.Name != nil, stringValue(input.Name),
		input.Description != nil, stringValue(input.Description),
		input.SelectionStrategy != nil, stringValue(input.SelectionStrategy),
	).Scan(&updatedID)
	if err != nil {
		return NodeGroup{}, mapWriteError(err)
	}
	return r.Get(ctx, updatedID)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM node_groups WHERE id = $1::uuid`, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrNodeGroupAssigned
		}
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) ListMembers(ctx context.Context, groupID string) ([]NodeGroupMember, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			m.node_group_id::text,
			m.server_id::text,
			s.name,
			s.vpn_protocol,
			s.deployment_role,
			m.priority,
			m.weight,
			m.enabled,
			m.created_at,
			m.updated_at
		FROM node_group_members m
		JOIN servers s ON s.id = m.server_id
		WHERE m.node_group_id = $1::uuid
		ORDER BY m.priority, lower(s.name), s.id
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]NodeGroupMember, 0)
	for rows.Next() {
		var member NodeGroupMember
		if err := rows.Scan(
			&member.NodeGroupID,
			&member.ServerID,
			&member.ServerName,
			&member.Protocol,
			&member.DeploymentRole,
			&member.Priority,
			&member.Weight,
			&member.Enabled,
			&member.CreatedAt,
			&member.UpdatedAt,
		); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *Repository) UpsertMember(ctx context.Context, input UpsertNodeGroupMemberInput) (NodeGroupMember, error) {
	var deploymentRole string
	if err := r.pool.QueryRow(ctx, `SELECT deployment_role FROM servers WHERE id = $1::uuid`, input.ServerID).Scan(&deploymentRole); err != nil {
		return NodeGroupMember{}, err
	}
	if deploymentRole != "vpn" && deploymentRole != "hybrid" {
		return NodeGroupMember{}, ErrServerNotVPNNode
	}
	if _, err := r.Get(ctx, input.NodeGroupID); err != nil {
		return NodeGroupMember{}, err
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO node_group_members (node_group_id, server_id, priority, weight, enabled)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		ON CONFLICT (node_group_id, server_id) DO UPDATE
		SET priority = EXCLUDED.priority,
			weight = EXCLUDED.weight,
			enabled = EXCLUDED.enabled,
			updated_at = now()
	`, input.NodeGroupID, input.ServerID, input.Priority, input.Weight, input.Enabled)
	if err != nil {
		return NodeGroupMember{}, err
	}
	return r.getMember(ctx, input.NodeGroupID, input.ServerID)
}

func (r *Repository) DeleteMember(ctx context.Context, groupID, serverID string) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM node_group_members
		WHERE node_group_id = $1::uuid AND server_id = $2::uuid
	`, groupID, serverID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) getMember(ctx context.Context, groupID, serverID string) (NodeGroupMember, error) {
	var member NodeGroupMember
	err := r.pool.QueryRow(ctx, `
		SELECT
			m.node_group_id::text,
			m.server_id::text,
			s.name,
			s.vpn_protocol,
			s.deployment_role,
			m.priority,
			m.weight,
			m.enabled,
			m.created_at,
			m.updated_at
		FROM node_group_members m
		JOIN servers s ON s.id = m.server_id
		WHERE m.node_group_id = $1::uuid AND m.server_id = $2::uuid
	`, groupID, serverID).Scan(
		&member.NodeGroupID,
		&member.ServerID,
		&member.ServerName,
		&member.Protocol,
		&member.DeploymentRole,
		&member.Priority,
		&member.Weight,
		&member.Enabled,
		&member.CreatedAt,
		&member.UpdatedAt,
	)
	return member, err
}

type candidateRow struct {
	candidate      NodeGroupCandidate
	deploymentRole string
	capabilities   []byte
	lastSeen       sql.NullTime
	load1          sql.NullFloat64
	logicalCPUs    sql.NullInt32
}

func (r *Repository) Candidates(ctx context.Context, groupID string, now time.Time) (ListNodeGroupCandidatesResponse, error) {
	group, err := r.Get(ctx, groupID)
	if err != nil {
		return ListNodeGroupCandidatesResponse{}, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			s.id::text,
			s.name,
			s.vpn_protocol,
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
	`, groupID)
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
		row.candidate.ProtocolSupported, row.candidate.RuntimeState = capabilityEvidence(row.capabilities, row.candidate.Protocol)
		row.candidate = evaluateCandidate(row.candidate, row.deploymentRole, now)
		response.Candidates = append(response.Candidates, row.candidate)
	}
	return response, rows.Err()
}

func scanNodeGroup(row interface{ Scan(...any) error }) (NodeGroup, error) {
	var group NodeGroup
	err := row.Scan(
		&group.ID,
		&group.Name,
		&group.Description,
		&group.SelectionStrategy,
		&group.MemberCount,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	return group, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "node_groups_name_ci_unique") {
		return ErrNodeGroupNameExists
	}
	return err
}

func capabilityEvidence(data []byte, protocol string) (bool, string) {
	var payload struct {
		VPNCores []struct {
			Type  string `json:"type"`
			State string `json:"state"`
		} `json:"vpnCores"`
		RouteGate struct {
			SchemaVersion   int `json:"schemaVersion"`
			VPNCoreAdapters []struct {
				Protocol string `json:"protocol"`
			} `json:"vpnCoreAdapters"`
		} `json:"routegate"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.RouteGate.SchemaVersion != 1 {
		return false, ""
	}
	supported := false
	for _, adapter := range payload.RouteGate.VPNCoreAdapters {
		if strings.EqualFold(strings.TrimSpace(adapter.Protocol), strings.TrimSpace(protocol)) {
			supported = true
			break
		}
	}
	coreType := protocolCore(protocol)
	runtimeState := ""
	for _, core := range payload.VPNCores {
		if strings.EqualFold(strings.TrimSpace(core.Type), coreType) {
			runtimeState = strings.ToLower(strings.TrimSpace(core.State))
			break
		}
	}
	return supported, runtimeState
}

func protocolCore(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless", "shadowsocks":
		return "sing-box"
	case "wireguard":
		return "wireguard"
	case "hysteria2":
		return "hysteria"
	case "mtproto":
		return "mtg"
	default:
		return ""
	}
}
