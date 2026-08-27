package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

func platformUpdateCapabilityJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"schemaVersion": PlatformUpdateCapabilitySchemaVersion,
		"state":         PlatformUpdateCapabilityStateReady,
		"request":       PlatformUpdateCapabilityRequestVersionOnly,
	})
}

// lockPlatformUpdateServer serializes the absence/presence check for active
// update jobs with update-job admission. Callers must pass the canonical UUID
// spelling and hold the transaction until their decision has been durably
// committed.
func lockPlatformUpdateServer(ctx context.Context, tx pgx.Tx, serverID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, serverID)
	return err
}

func (r *Repository) CreatePlatformUpdateJob(ctx context.Context, input CreatePlatformUpdateJobInput) (PlatformUpdateJob, error) {
	serverID, err := canonicalPlatformUpdateServerID(input.ServerID)
	if err != nil {
		return PlatformUpdateJob{}, fmt.Errorf("invalid server id: %w", err)
	}
	if !validPlatformUpdateTargetVersion(input.TargetVersion) {
		return PlatformUpdateJob{}, fmt.Errorf("invalid RouteGate target version")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PlatformUpdateJob{}, err
	}
	defer tx.Rollback(ctx)

	job, err := createPlatformUpdateJobTx(ctx, tx, CreatePlatformUpdateJobInput{
		ServerID:      serverID,
		TargetVersion: input.TargetVersion,
	})
	if err != nil {
		return PlatformUpdateJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PlatformUpdateJob{}, err
	}
	return job, nil
}

// createPlatformUpdateJobTx is the authoritative single-node mutation
// admission primitive for callers that must bind the resulting job atomically
// with other durable control-plane state. Inputs must already be canonical and
// validated by the caller; the function deliberately reuses the exact SQL
// predicates and error semantics of the public single-node API.
func createPlatformUpdateJobTx(ctx context.Context, tx pgx.Tx, input CreatePlatformUpdateJobInput) (PlatformUpdateJob, error) {
	if err := lockPlatformUpdateServer(ctx, tx, input.ServerID); err != nil {
		return PlatformUpdateJob{}, err
	}
	capability, err := platformUpdateCapabilityJSON()
	if err != nil {
		return PlatformUpdateJob{}, err
	}

	return scanPlatformUpdateJob(tx.QueryRow(ctx, `
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
	`, input.ServerID, input.TargetVersion, capability))
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
