package servers

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateServer(ctx context.Context, input CreateServerInput) (Server, error) {
	status := input.Status
	if status == "" {
		status = StatusPending
	}
	return scanServer(r.pool.QueryRow(ctx, `
		INSERT INTO servers (
			name, description, location, provider, public_ip, private_ip, status
		)
		VALUES (
			$1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''),
			NULLIF($5, '')::inet, NULLIF($6, '')::inet, $7
		)
		RETURNING
			id::text, name, COALESCE(description, ''), COALESCE(location, ''),
			COALESCE(provider, ''), COALESCE(public_ip::text, ''),
			COALESCE(private_ip::text, ''), status, created_at, updated_at,
			COALESCE(hostname, '')
	`, input.Name, input.Description, input.Location, input.Provider, input.PublicIP, input.PrivateIP, status))
}

func (r *Repository) ListServers(ctx context.Context, filter ServerFilter) ([]Server, error) {
	rows, err := r.pool.Query(ctx, serverSelect+`
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR provider = $2)
		  AND ($3 = '' OR location = $3)
		  AND ($4 = '' OR name ILIKE '%' || $4 || '%')
		ORDER BY created_at DESC
		LIMIT CASE WHEN $5 > 0 THEN $5 ELSE 100 END
		OFFSET CASE WHEN $6 > 0 THEN $6 ELSE 0 END
	`, filter.Status, filter.Provider, filter.Location, filter.Search, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Server, 0)
	for rows.Next() {
		item, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetServerByID(ctx context.Context, id string) (Server, error) {
	return scanServer(r.pool.QueryRow(ctx, serverSelect+` WHERE id = $1::uuid`, id))
}

func (r *Repository) UpdateServer(ctx context.Context, id string, input UpdateServerInput) (Server, error) {
	return scanServer(r.pool.QueryRow(ctx, `
		UPDATE servers
		SET
			name = CASE WHEN $2 THEN $3 ELSE name END,
			description = CASE WHEN $4 THEN NULLIF($5, '') ELSE description END,
			location = CASE WHEN $6 THEN NULLIF($7, '') ELSE location END,
			provider = CASE WHEN $8 THEN NULLIF($9, '') ELSE provider END,
			public_ip = CASE WHEN $10 THEN NULLIF($11, '')::inet ELSE public_ip END,
			private_ip = CASE WHEN $12 THEN NULLIF($13, '')::inet ELSE private_ip END,
			status = CASE WHEN $14 THEN $15 ELSE status END,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING
			id::text, name, COALESCE(description, ''), COALESCE(location, ''),
			COALESCE(provider, ''), COALESCE(public_ip::text, ''),
			COALESCE(private_ip::text, ''), status, created_at, updated_at,
			COALESCE(hostname, '')
	`,
		id,
		input.Name != nil, stringValue(input.Name),
		input.Description != nil, stringValue(input.Description),
		input.Location != nil, stringValue(input.Location),
		input.Provider != nil, stringValue(input.Provider),
		input.PublicIP != nil, stringValue(input.PublicIP),
		input.PrivateIP != nil, stringValue(input.PrivateIP),
		input.Status != nil, stringValue(input.Status),
	))
}

func (r *Repository) DeleteServer(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM servers WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) GetServerWithAgent(ctx context.Context, serverID string) (ServerWithAgent, error) {
	return scanServerWithAgent(r.pool.QueryRow(ctx, serverWithAgentSelect+` WHERE s.id = $1::uuid`, serverID))
}

func (r *Repository) ListServersWithAgent(ctx context.Context, filter ServerFilter) ([]ServerWithAgent, error) {
	rows, err := r.pool.Query(ctx, serverWithAgentSelect+`
		WHERE ($1 = '' OR s.status = $1)
		  AND ($2 = '' OR s.provider = $2)
		  AND ($3 = '' OR s.location = $3)
		  AND ($4 = '' OR s.name ILIKE '%' || $4 || '%')
		ORDER BY s.created_at DESC
		LIMIT CASE WHEN $5 > 0 THEN $5 ELSE 100 END
		OFFSET CASE WHEN $6 > 0 THEN $6 ELSE 0 END
	`, filter.Status, filter.Provider, filter.Location, filter.Search, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServerWithAgent, 0)
	for rows.Next() {
		item, err := scanServerWithAgent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// List is retained for the existing service until the HTTP layer is migrated.
func (r *Repository) List(ctx context.Context) ([]Server, error) {
	return r.ListServers(ctx, ServerFilter{})
}

// Create adapts the legacy API request to CreateServer.
func (r *Repository) Create(ctx context.Context, request CreateServerRequest) (Server, error) {
	return scanServer(r.pool.QueryRow(ctx, `
		INSERT INTO servers (
			name, hostname, public_ip, location, provider, status
		)
		VALUES (
			$1, NULLIF($2, ''), NULLIF($3, '')::inet,
			NULLIF($4, ''), NULLIF($5, ''), 'pending'
		)
		RETURNING
			id::text, name, COALESCE(description, ''), COALESCE(location, ''),
			COALESCE(provider, ''), COALESCE(public_ip::text, ''),
			COALESCE(private_ip::text, ''), status, created_at, updated_at,
			COALESCE(hostname, '')
	`, request.Name, request.Hostname, request.PublicIP, request.Location, request.Provider))
}

func (r *Repository) SeedDemo(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO servers (name, hostname, public_ip, location, provider, status)
		VALUES (
			'Demo Finland VPS', 'fi-demo.routegate.local', '203.0.113.10'::inet,
			'Finland', 'Demo', 'active'
		)
	`)
	return err
}

const serverSelect = `
	SELECT
		id::text,
		name,
		COALESCE(description, ''),
		COALESCE(location, ''),
		COALESCE(provider, ''),
		COALESCE(public_ip::text, ''),
		COALESCE(private_ip::text, ''),
		status,
		created_at,
		updated_at,
		COALESCE(hostname, '')
	FROM servers`

const serverWithAgentSelect = `
	SELECT
		s.id::text,
		s.name,
		COALESCE(s.description, ''),
		COALESCE(s.location, ''),
		COALESCE(s.provider, ''),
		COALESCE(s.public_ip::text, ''),
		COALESCE(s.private_ip::text, ''),
		s.status,
		s.created_at,
		s.updated_at,
		COALESCE(s.hostname, ''),
		a.id::text,
		a.server_id::text,
		a.hostname,
		a.os,
		a.arch,
		a.agent_version,
		a.protocol_version,
		a.status,
		a.token_hash,
		a.capabilities,
		a.registered_at,
		a.last_seen_at,
		a.created_at,
		a.updated_at,
		a.name
	FROM servers s
	LEFT JOIN agents a ON a.server_id = s.id`

type scanner interface {
	Scan(dest ...any) error
}

func scanServer(row scanner) (Server, error) {
	var server Server
	err := row.Scan(
		&server.ID,
		&server.Name,
		&server.Description,
		&server.Location,
		&server.Provider,
		&server.PublicIP,
		&server.PrivateIP,
		&server.Status,
		&server.CreatedAt,
		&server.UpdatedAt,
		&server.Hostname,
	)
	return server, err
}

func scanServerWithAgent(row scanner) (ServerWithAgent, error) {
	var result ServerWithAgent
	var agentID, serverID, hostname, osName, arch, version sql.NullString
	var status, tokenHash, name sql.NullString
	var protocolVersion sql.NullInt32
	var registeredAt, lastSeenAt, createdAt, updatedAt sql.NullTime
	var capabilities []byte

	err := row.Scan(
		&result.Server.ID,
		&result.Server.Name,
		&result.Server.Description,
		&result.Server.Location,
		&result.Server.Provider,
		&result.Server.PublicIP,
		&result.Server.PrivateIP,
		&result.Server.Status,
		&result.Server.CreatedAt,
		&result.Server.UpdatedAt,
		&result.Server.Hostname,
		&agentID,
		&serverID,
		&hostname,
		&osName,
		&arch,
		&version,
		&protocolVersion,
		&status,
		&tokenHash,
		&capabilities,
		&registeredAt,
		&lastSeenAt,
		&createdAt,
		&updatedAt,
		&name,
	)
	if err != nil {
		return ServerWithAgent{}, err
	}
	if !agentID.Valid {
		return result, nil
	}

	agent := agents.Agent{
		ID:           agentID.String,
		ServerID:     serverID.String,
		Hostname:     hostname.String,
		OS:           osName.String,
		Arch:         arch.String,
		AgentVersion: version.String,
		Version:      version.String,
		Status:       status.String,
		TokenHash:    tokenHash.String,
		RegisteredAt: registeredAt.Time,
		CreatedAt:    createdAt.Time,
		UpdatedAt:    updatedAt.Time,
		Name:         name.String,
	}
	if protocolVersion.Valid {
		value := int(protocolVersion.Int32)
		agent.ProtocolVersion = &value
	}
	agent.Compatibility = agents.EvaluateCompatibility(agent.AgentVersion, agent.ProtocolVersion)
	if len(capabilities) > 0 {
		if err := json.Unmarshal(capabilities, &agent.Capabilities); err != nil {
			return ServerWithAgent{}, err
		}
	}
	if lastSeenAt.Valid {
		agent.LastSeenAt = &lastSeenAt.Time
		agent.LastSeen = lastSeenAt.Time
	}
	result.Agent = &agent
	return result, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
