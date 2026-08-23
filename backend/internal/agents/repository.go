package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
			protocol_version,
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
			$9,
			COALESCE($10, now()),
			$11
		)
		ON CONFLICT (server_id)
		DO UPDATE SET
			hostname = EXCLUDED.hostname,
			os = EXCLUDED.os,
			arch = EXCLUDED.arch,
			agent_version = EXCLUDED.agent_version,
			version = EXCLUDED.version,
			protocol_version = EXCLUDED.protocol_version,
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
			protocol_version,
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
		input.ProtocolVersion,
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
				protocol_version = COALESCE($3, protocol_version),
				capabilities = COALESCE($4, capabilities),
				updated_at = now()
			WHERE token_hash = $1
			RETURNING
				id::text, server_id::text, COALESCE(hostname, ''),
				COALESCE(os, ''), COALESCE(arch, ''), agent_version, protocol_version,
				status, token_hash, capabilities, registered_at,
				last_seen_at, created_at, updated_at, name;
		`, input.TokenHash, optionalString(input.AgentVersion), input.ProtocolVersion, optionalCapabilities(input.Capabilities))
	} else {
		row = tx.QueryRow(ctx, `
			UPDATE agents
			SET
				last_seen_at = now(),
				status = 'online',
				agent_version = COALESCE(NULLIF($2, ''), agent_version),
				version = COALESCE(NULLIF($2, ''), version),
				protocol_version = COALESCE($3, protocol_version),
				capabilities = COALESCE($4, capabilities),
				updated_at = now()
			WHERE id = $1::uuid
			RETURNING
				id::text, server_id::text, COALESCE(hostname, ''),
				COALESCE(os, ''), COALESCE(arch, ''), agent_version, protocol_version,
				status, token_hash, capabilities, registered_at,
				last_seen_at, created_at, updated_at, name;
		`, input.AgentID, optionalString(input.AgentVersion), input.ProtocolVersion, optionalCapabilities(input.Capabilities))
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

	// The Agent runner sends its heartbeat before it claims the next task and
	// processes tasks sequentially. If a task assigned to this Agent is still
	// in_progress for several minutes when a later heartbeat arrives, the
	// previous execution was interrupted or its completion report never reached
	// Manager. Close that orphan instead of leaving the dashboard and job history
	// permanently stuck in an in-progress state. The grace period also protects
	// against an accidentally duplicated Agent process using the same token.
	if _, err := tx.Exec(ctx, `
		UPDATE config_apply_jobs
		SET
			status = 'failed',
			error_message = COALESCE(
				NULLIF(error_message, ''),
				'Agent task completion was not confirmed before a later heartbeat.'
			),
			completed_at = COALESCE(completed_at, now()),
			updated_at = now()
		WHERE agent_id = $1::uuid
		  AND status = 'in_progress'
		  AND started_at IS NOT NULL
		  AND started_at < now() - interval '5 minutes'
	`, agent.ID); err != nil {
		return Agent{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE agent_operation_jobs
		SET
			status = 'failed',
			error_message = COALESCE(
				NULLIF(error_message, ''),
				'Agent operation completion was not confirmed before a later heartbeat.'
			),
			completed_at = COALESCE(completed_at, now()),
			updated_at = now()
		WHERE agent_id = $1::uuid
		  AND status = 'in_progress'
		  AND started_at IS NOT NULL
		  AND started_at < now() - interval '5 minutes'
	`, agent.ID); err != nil {
		return Agent{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (r *Repository) ClaimNextConfigTask(ctx context.Context, tokenHash string) (*AgentConfigTask, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var agent Agent
	if agent, err = scanAgent(tx.QueryRow(ctx, agentSelect+` WHERE token_hash = $1`, tokenHash)); err != nil {
		return nil, err
	}

	task, err := scanAgentConfigTask(tx.QueryRow(ctx, `
		WITH next_job AS (
			SELECT j.id
			FROM config_apply_jobs j
			WHERE j.server_id = $1::uuid
			  AND (j.agent_id IS NULL OR j.agent_id = $2::uuid)
			  AND j.status = 'pending'
			ORDER BY j.created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE config_apply_jobs j
		SET
			agent_id = $2::uuid,
			status = 'in_progress',
			started_at = COALESCE(j.started_at, now()),
			updated_at = now()
		FROM next_job, config_versions cv
		WHERE j.id = next_job.id
		  AND cv.id = j.config_version_id
		RETURNING
			j.id::text,
			j.server_id::text,
			j.agent_id::text,
			j.config_version_id::text,
			j.action,
			j.status,
			cv.rendered_config,
			cv.config_hash,
			j.created_at,
			j.started_at
	`, agent.ServerID, agent.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *Repository) CompleteConfigTask(ctx context.Context, input CompleteConfigTaskInput) error {
	resultPayload := input.ResultPayload
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	payloadBytes, err := json.Marshal(resultPayload)
	if err != nil {
		return err
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE config_apply_jobs j
		SET
			status = $3,
			result_payload = $4::jsonb,
			error_message = NULLIF($5, ''),
			completed_at = now(),
			updated_at = now()
		FROM agents a
		WHERE j.id = $1::uuid
		  AND a.token_hash = $2
		  AND j.agent_id = a.id
		  AND j.status = 'in_progress'
	`, input.JobID, input.TokenHash, input.Status, payloadBytes, strings.TrimSpace(input.ErrorMessage))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
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

const agentSelect = `
	SELECT
		id::text,
		server_id::text,
		COALESCE(hostname, ''),
		COALESCE(os, ''),
		COALESCE(arch, ''),
		agent_version,
		protocol_version,
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
	var protocolVersion sql.NullInt32
	err := row.Scan(
		&agent.ID,
		&agent.ServerID,
		&agent.Hostname,
		&agent.OS,
		&agent.Arch,
		&agent.AgentVersion,
		&protocolVersion,
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
	if protocolVersion.Valid {
		value := int(protocolVersion.Int32)
		agent.ProtocolVersion = &value
	}
	agent.Capabilities = Capabilities{}
	if len(capabilities) > 0 {
		if err := json.Unmarshal(capabilities, &agent.Capabilities); err != nil {
			return Agent{}, err
		}
	}
	agent.Version = agent.AgentVersion
	agent.Compatibility = EvaluateCompatibility(agent.AgentVersion, agent.ProtocolVersion)
	if agent.LastSeenAt != nil {
		agent.LastSeen = *agent.LastSeenAt
	}
	return agent, nil
}

func scanAgentConfigTask(row scanner) (AgentConfigTask, error) {
	var task AgentConfigTask
	var renderedConfig []byte
	var startedAt sql.NullTime
	err := row.Scan(
		&task.ID,
		&task.ServerID,
		&task.AgentID,
		&task.ConfigVersionID,
		&task.Action,
		&task.Status,
		&renderedConfig,
		&task.ConfigHash,
		&task.CreatedAt,
		&startedAt,
	)
	if err != nil {
		return AgentConfigTask{}, err
	}
	task.RenderedConfig = renderedConfig
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	return task, nil
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
