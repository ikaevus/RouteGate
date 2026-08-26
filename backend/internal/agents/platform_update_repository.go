package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	PlatformUpdateCapabilityStateReady         = "ready"
	PlatformUpdateCapabilityRequestVersionOnly = "version_only"
	PlatformUpdateCapabilitySchemaVersion      = 1
)

type PlatformUpdateJob struct {
	ID            string     `json:"id"`
	ServerID      string     `json:"serverId"`
	TargetVersion string     `json:"targetVersion"`
	Status        string     `json:"status"`
	ErrorCode     string     `json:"errorCode,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	DispatchedAt  *time.Time `json:"dispatchedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

type CreatePlatformUpdateJobInput struct {
	ServerID      string
	TargetVersion string
}

func (r *Repository) CreatePlatformUpdateJob(ctx context.Context, input CreatePlatformUpdateJobInput) (PlatformUpdateJob, error) {
	serverID := strings.TrimSpace(input.ServerID)
	targetVersion := input.TargetVersion
	if serverID == "" {
		return PlatformUpdateJob{}, fmt.Errorf("server id is required")
	}
	if !validPlatformUpdateTargetVersion(targetVersion) {
		return PlatformUpdateJob{}, fmt.Errorf("invalid RouteGate target version")
	}
	capability, err := json.Marshal(map[string]any{
		"schemaVersion": PlatformUpdateCapabilitySchemaVersion,
		"state":         PlatformUpdateCapabilityStateReady,
		"request":       PlatformUpdateCapabilityRequestVersionOnly,
	})
	if err != nil {
		return PlatformUpdateJob{}, err
	}

	return scanPlatformUpdateJob(r.pool.QueryRow(ctx, `
		INSERT INTO agent_platform_update_jobs (server_id, agent_id, target_version)
		SELECT s.id, a.id, $2
		FROM servers s
		JOIN LATERAL (
			SELECT a.id, a.capabilities
			FROM agents a
			WHERE a.server_id = s.id
			  AND a.status <> 'disabled'
			ORDER BY a.updated_at DESC, a.id DESC
			LIMIT 1
		) a ON true
		WHERE s.id = $1::uuid
		  AND s.status <> 'disabled'
		  AND s.deployment_role = 'vpn'
		  AND a.capabilities -> 'softwareUpdate' = $3::jsonb
		RETURNING
			id::text,
			server_id::text,
			target_version,
			status,
			COALESCE(error_code, ''),
			created_at,
			updated_at,
			started_at,
			dispatched_at,
			completed_at
	`, serverID, targetVersion, capability))
}

func (r *Repository) GetPlatformUpdateJob(ctx context.Context, serverID, jobID string) (PlatformUpdateJob, error) {
	serverID = strings.TrimSpace(serverID)
	jobID = strings.TrimSpace(jobID)
	if serverID == "" || jobID == "" {
		return PlatformUpdateJob{}, fmt.Errorf("server id and job id are required")
	}
	return scanPlatformUpdateJob(r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			server_id::text,
			target_version,
			status,
			COALESCE(error_code, ''),
			created_at,
			updated_at,
			started_at,
			dispatched_at,
			completed_at
		FROM agent_platform_update_jobs
		WHERE id = $1::uuid
		  AND server_id = $2::uuid
	`, jobID, serverID))
}

func scanPlatformUpdateJob(row scanner) (PlatformUpdateJob, error) {
	var job PlatformUpdateJob
	var startedAt, dispatchedAt, completedAt sql.NullTime
	if err := row.Scan(
		&job.ID,
		&job.ServerID,
		&job.TargetVersion,
		&job.Status,
		&job.ErrorCode,
		&job.CreatedAt,
		&job.UpdatedAt,
		&startedAt,
		&dispatchedAt,
		&completedAt,
	); err != nil {
		return PlatformUpdateJob{}, err
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if dispatchedAt.Valid {
		job.DispatchedAt = &dispatchedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return job, nil
}
