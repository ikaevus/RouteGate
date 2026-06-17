package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrLegacyRegistrationDisabled = errors.New("legacy direct agent registration is disabled; use the registration-token flow")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) GetAgentByID(ctx context.Context, id string) (Agent, error) {
	return scanAgent(r.pool.QueryRow(ctx, agentSelect+` WHERE id = $1::uuid`, id))
}

func (r *Repository) GetAgentByServerID(ctx context.Context, serverID string) (Agent, error) {
	return scanAgent(r.pool.QueryRow(ctx, agentSelect+` WHERE server_id = $1::uuid`, serverID))
}

func (r *Repository) FindAgentByTokenHash(ctx context.Context, tokenHash string) (Agent, error) {
	return scanAgent(r.pool.QueryRow(ctx, agentSelect+` WHERE token_hash = $1`, tokenHash))
}

func (r *Repository) CreateOrReplaceAgentForServer(ctx context.Context, input CreateOrReplaceAgentInput) (Agent, error) {
	status := input.Status
	if status == "" {
		status = StatusRegistered
	}
	capabilities := input.Capabilities
	if capabilities == nil {
		capabilities = Capabilities{}
	}

	return scanAgent(r.pool.QueryRow(ctx, `
		INSERT INTO agents (
			server_id,
			hostname,
			os,
			arch,
			agent_version,
			version,
			status,
			token_hash,
			capabilities,
			registered_at,
			last_seen_at
		)
		VALUES (
			$1::uuid,
			NULLIF($2, ''),
			NULLIF($3, ''),
			NULLIF($4, ''),
			$5,
			$5,
			$6,
			$7,
			$8,
			COALESCE($9, now()),
			$10
		)
		ON CONFLICT (server_id)
		DO UPDATE SET
			hostname = EXCLUDED.hostname,
			os = EXCLUDED.os,
			arch = EXCLUDED.arch,
			agent_version = EXCLUDED.agent_version,
			version = EXCLUDED.version,
			status = EXCLUDED.status,
			token_hash = EXCLUDED.token_hash,
			capabilities = EXCLUDED.capabilities,
			registered_at = EXCLUDED.registered_at,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = now()
		RETURNING
			id::text,
			server_id::text,
			COALESCE(hostname, ''),
			COALESCE(os, ''),
			COALESCE(arch, ''),
			agent_version,
			status,
			token_hash,
			capabilities,
			registered_at,
			last_seen_at,
			created_at,
			updated_at,
			name;
	`,
		input.ServerID,
		input.Hostname,
		input.OS,
		input.Arch,
		input.AgentVersion,
		status,
		input.TokenHash,
		capabilities,
		input.RegisteredAt,
		input.LastSeenAt,
	))
}

func (r *Repository) UpdateAgentHeartbeat(ctx context.Context, input UpdateAgentHeartbeatInput) (Agent, error) {
	if input.AgentID == "" && input.TokenHash == "" {
		return Agent{}, ErrAgentIDRequired
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback(ctx)

	var row pgx.Row
	if input.TokenHash != "" {
		row = tx.QueryRow(ctx, `
			UPDATE agents
			SET
				last_seen_at = now(),
				status = 'online',
				agent_version = COALESCE(NULLIF($2, ''), agent_version),
				version = COALESCE(NULLIF($2, ''), version),
				capabilities = COALESCE($3, capabilities),
				updated_at = now()
			WHERE token_hash = $1
			RETURNING
				id::text, server_id::text, COALESCE(hostname, ''),
				COALESCE(os, ''), COALESCE(arch, ''), agent_version,
				status, token_hash, capabilities, registered_at,
				last_seen_at, created_at, updated_at, name;
		`, input.TokenHash, optionalString(input.AgentVersion), optionalCapabilities(input.Capabilities))
	} else {
		row = tx.QueryRow(ctx, `
			UPDATE agents
			SET
				last_seen_at = now(),
				status = 'online',
				agent_version = COALESCE(NULLIF($2, ''), agent_version),
				version = COALESCE(NULLIF($2, ''), version),
				capabilities = COALESCE($3, capabilities),
				updated_at = now()
			WHERE id = $1::uuid
			RETURNING
				id::text, server_id::text, COALESCE(hostname, ''),
				COALESCE(os, ''), COALESCE(arch, ''), agent_version,
				status, token_hash, capabilities, registered_at,
				last_seen_at, created_at, updated_at, name;
		`, input.AgentID, optionalString(input.AgentVersion), optionalCapabilities(input.Capabilities))
	}

	agent, err := scanAgent(row)
	if err != nil {
		return Agent{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE servers
		SET status = 'active', updated_at = now()
		WHERE id = $1::uuid
	`, agent.ServerID); err != nil {
		return Agent{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (r *Repository) CreateRegistrationToken(ctx context.Context, input CreateRegistrationTokenInput) (ServerRegistrationToken, error) {
	var token ServerRegistrationToken
	err := r.pool.QueryRow(ctx, `
		INSERT INTO server_registration_tokens (server_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)
		RETURNING id::text, server_id::text, token_hash, expires_at, used_at, created_at
	`, input.ServerID, input.TokenHash, input.ExpiresAt).Scan(
		&token.ID,
		&token.ServerID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.UsedAt,
		&token.CreatedAt,
	)
	return token, err
}

func (r *Repository) ConsumeValidRegistrationTokenByHash(ctx context.Context, tokenHash string) (ServerRegistrationToken, error) {
	var token ServerRegistrationToken
	err := r.pool.QueryRow(ctx, `
		UPDATE server_registration_tokens
		SET used_at = now()
		WHERE token_hash = $1
		  AND used_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		RETURNING id::text, server_id::text, token_hash, expires_at, used_at, created_at
	`, tokenHash).Scan(
		&token.ID,
		&token.ServerID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.UsedAt,
		&token.CreatedAt,
	)
	return token, err
}

func (r *Repository) ActivateServer(ctx context.Context, serverID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE servers
		SET status = 'active', updated_at = now()
		WHERE id = $1::uuid
	`, serverID)
	return err
}

// List is retained for the existing service until the HTTP layer is migrated.
func (r *Repository) List(ctx context.Context) ([]Agent, error) {
	rows, err := r.pool.Query(ctx, agentSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Agent, 0)
	for rows.Next() {
		item, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Register is retained for API compatibility, but direct registration cannot
// safely persist an agent without a caller-provided token hash.
func (r *Repository) Register(_ context.Context, _ RegisterAgentRequest) (Agent, error) {
	return Agent{}, ErrLegacyRegistrationDisabled
}

// Heartbeat adapts the legacy API request to UpdateAgentHeartbeat.
func (r *Repository) Heartbeat(ctx context.Context, request HeartbeatRequest) (time.Time, bool, error) {
	version := strings.TrimSpace(request.Version)
	agent, err := r.UpdateAgentHeartbeat(ctx, UpdateAgentHeartbeatInput{
		AgentID:      request.AgentID,
		AgentVersion: &version,
	})
	if err == pgx.ErrNoRows {
		return time.Now().UTC(), false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if agent.LastSeenAt == nil {
		return time.Now().UTC(), true, nil
	}
	return *agent.LastSeenAt, true, nil
}

func (r *Repository) SeedDemo(context.Context) error {
	return nil
}

const agentSelect = `
	SELECT
		id::text,
		server_id::text,
		COALESCE(hostname, ''),
		COALESCE(os, ''),
		COALESCE(arch, ''),
		agent_version,
		status,
		token_hash,
		capabilities,
		registered_at,
		last_seen_at,
		created_at,
		updated_at,
		name
	FROM agents`

type scanner interface {
	Scan(dest ...any) error
}

func scanAgent(row scanner) (Agent, error) {
	var agent Agent
	var capabilities []byte
	err := row.Scan(
		&agent.ID,
		&agent.ServerID,
		&agent.Hostname,
		&agent.OS,
		&agent.Arch,
		&agent.AgentVersion,
		&agent.Status,
		&agent.TokenHash,
		&capabilities,
		&agent.RegisteredAt,
		&agent.LastSeenAt,
		&agent.CreatedAt,
		&agent.UpdatedAt,
		&agent.Name,
	)
	if err != nil {
		return Agent{}, err
	}
	agent.Capabilities = Capabilities{}
	if len(capabilities) > 0 {
		if err := json.Unmarshal(capabilities, &agent.Capabilities); err != nil {
			return Agent{}, err
		}
	}
	agent.Version = agent.AgentVersion
	if agent.LastSeenAt != nil {
		agent.LastSeen = *agent.LastSeenAt
	}
	return agent, nil
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func optionalCapabilities(value Capabilities) any {
	if value == nil {
		return nil
	}
	return value
}
