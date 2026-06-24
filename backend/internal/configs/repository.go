package configs

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) GetServerConfigInfo(ctx context.Context, serverID string) (ServerConfigInfo, error) {
	return scanServerConfigInfo(r.pool.QueryRow(ctx, `
		SELECT
			s.id::text,
			s.name,
			COALESCE(s.hostname, ''),
			COALESCE(s.public_ip::text, ''),
			COALESCE(s.private_ip::text, ''),
			COALESCE(s.location, ''),
			COALESCE(s.provider, ''),
			s.status,
			a.id::text,
			COALESCE(a.hostname, ''),
			COALESCE(a.os, ''),
			COALESCE(a.arch, ''),
			COALESCE(a.agent_version, ''),
			COALESCE(a.status, ''),
			a.capabilities
		FROM servers s
		LEFT JOIN agents a ON a.server_id = s.id
		WHERE s.id = $1::uuid
	`, serverID))
}

func (r *Repository) CreateConfigVersion(ctx context.Context, input CreateConfigVersionInput) (ConfigVersion, error) {
	configBytes, err := json.Marshal(input.RenderedConfig)
	if err != nil {
		return ConfigVersion{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ConfigVersion{}, err
	}
	defer tx.Rollback(ctx)

	var lockedID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM servers WHERE id = $1::uuid FOR UPDATE`, input.ServerID).Scan(&lockedID); err != nil {
		return ConfigVersion{}, err
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM config_versions
		WHERE server_id = $1::uuid
	`, input.ServerID).Scan(&nextVersion); err != nil {
		return ConfigVersion{}, err
	}

	version, err := scanConfigVersion(tx.QueryRow(ctx, `
		INSERT INTO config_versions (
			server_id,
			version,
			config_hash,
			status,
			rendered_config
		)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb)
		RETURNING
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at
	`, input.ServerID, nextVersion, input.ConfigHash, input.Status, configBytes))
	if err != nil {
		return ConfigVersion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ConfigVersion{}, err
	}
	return version, nil
}

func (r *Repository) ListConfigVersions(ctx context.Context, serverID string) ([]ConfigVersion, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at
		FROM config_versions
		WHERE server_id = $1::uuid
		ORDER BY version DESC
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ConfigVersion{}
	for rows.Next() {
		item, err := scanConfigVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetConfigVersion(ctx context.Context, serverID, versionID string) (ConfigVersion, error) {
	return scanConfigVersion(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at
		FROM config_versions
		WHERE server_id = $1::uuid
		  AND id = $2::uuid
	`, serverID, versionID))
}

func (r *Repository) MarkConfigVersionValidated(ctx context.Context, serverID, versionID string) (ConfigVersion, error) {
	return scanConfigVersion(r.pool.QueryRow(ctx, `
		UPDATE config_versions
		SET status = $3
		WHERE server_id = $1::uuid
		  AND id = $2::uuid
		RETURNING
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at
	`, serverID, versionID, StatusValidated))
}

func (r *Repository) CreateConfigApplyJob(ctx context.Context, input CreateConfigApplyJobInput) (ConfigApplyJob, error) {
	action := input.Action
	if action == "" {
		action = ApplyJobActionApply
	}
	requestPayload := input.RequestPayload
	if requestPayload == nil {
		requestPayload = map[string]any{}
	}

	return scanConfigApplyJob(r.pool.QueryRow(ctx, `
		INSERT INTO config_apply_jobs (
			server_id,
			agent_id,
			config_version_id,
			action,
			status,
			request_payload
		)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, $3::uuid, $4, $5, $6::jsonb)
		RETURNING
			id::text,
			server_id::text,
			COALESCE(agent_id::text, ''),
			config_version_id::text,
			action,
			status,
			request_payload,
			result_payload,
			COALESCE(error_message, ''),
			created_at,
			updated_at,
			started_at,
			completed_at
	`, input.ServerID, input.AgentID, input.ConfigVersionID, action, ApplyJobStatusPending, requestPayload))
}

func (r *Repository) ListConfigApplyJobs(ctx context.Context, serverID string) ([]ConfigApplyJob, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			server_id::text,
			COALESCE(agent_id::text, ''),
			config_version_id::text,
			action,
			status,
			request_payload,
			result_payload,
			COALESCE(error_message, ''),
			created_at,
			updated_at,
			started_at,
			completed_at
		FROM config_apply_jobs
		WHERE server_id = $1::uuid
		ORDER BY created_at DESC
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ConfigApplyJob{}
	for rows.Next() {
		item, err := scanConfigApplyJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetConfigApplyJob(ctx context.Context, serverID, jobID string) (ConfigApplyJob, error) {
	return scanConfigApplyJob(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			server_id::text,
			COALESCE(agent_id::text, ''),
			config_version_id::text,
			action,
			status,
			request_payload,
			result_payload,
			COALESCE(error_message, ''),
			created_at,
			updated_at,
			started_at,
			completed_at
		FROM config_apply_jobs
		WHERE server_id = $1::uuid
		  AND id = $2::uuid
	`, serverID, jobID))
}

func scanServerConfigInfo(row pgx.Row) (ServerConfigInfo, error) {
	var info ServerConfigInfo
	var agentID sql.NullString
	var agentHostname sql.NullString
	var agentOS sql.NullString
	var agentArch sql.NullString
	var agentVersion sql.NullString
	var agentStatus sql.NullString
	var capabilitiesBytes []byte

	err := row.Scan(
		&info.ID,
		&info.Name,
		&info.Hostname,
		&info.PublicIP,
		&info.PrivateIP,
		&info.Location,
		&info.Provider,
		&info.Status,
		&agentID,
		&agentHostname,
		&agentOS,
		&agentArch,
		&agentVersion,
		&agentStatus,
		&capabilitiesBytes,
	)
	if err != nil {
		return ServerConfigInfo{}, err
	}
	if !agentID.Valid {
		return info, nil
	}

	agent := &AgentConfigInfo{
		ID:           agentID.String,
		Hostname:     agentHostname.String,
		OS:           agentOS.String,
		Arch:         agentArch.String,
		AgentVersion: agentVersion.String,
		Status:       agentStatus.String,
		Capabilities: map[string]any{},
	}
	if len(capabilitiesBytes) > 0 {
		if err := json.Unmarshal(capabilitiesBytes, &agent.Capabilities); err != nil {
			return ServerConfigInfo{}, err
		}
	}
	info.Agent = agent
	return info, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanConfigVersion(row scanner) (ConfigVersion, error) {
	var version ConfigVersion
	var renderedConfig []byte
	err := row.Scan(
		&version.ID,
		&version.ServerID,
		&version.Version,
		&version.ConfigHash,
		&version.Status,
		&renderedConfig,
		&version.CreatedAt,
		&version.AppliedAt,
	)
	if err != nil {
		return ConfigVersion{}, err
	}
	version.RenderedConfig = renderedConfig
	return version, nil
}

func scanConfigApplyJob(row scanner) (ConfigApplyJob, error) {
	var job ConfigApplyJob
	var requestPayload []byte
	var resultPayload []byte
	err := row.Scan(
		&job.ID,
		&job.ServerID,
		&job.AgentID,
		&job.ConfigVersionID,
		&job.Action,
		&job.Status,
		&requestPayload,
		&resultPayload,
		&job.ErrorMessage,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.CompletedAt,
	)
	if err != nil {
		return ConfigApplyJob{}, err
	}
	job.RequestPayload = requestPayload
	job.ResultPayload = resultPayload
	return job, nil
}
