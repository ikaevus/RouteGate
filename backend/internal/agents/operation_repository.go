package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	AgentOperationJobStatusPending    = "pending"
	AgentOperationJobStatusInProgress = "in_progress"
	AgentOperationJobStatusSucceeded  = "succeeded"
	AgentOperationJobStatusFailed     = "failed"
)

type CreateAgentOperationJobInput struct {
	ServerID  string
	Operation string
}

type CompleteAgentOperationJobInput struct {
	TokenHash     string
	JobID         string
	Status        string
	ErrorMessage  string
	ResultPayload map[string]any
}

func ValidVPNCoreOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case VPNCoreOperationStart, VPNCoreOperationStop, VPNCoreOperationRestart:
		return true
	default:
		return false
	}
}

func (r *Repository) CreateAgentOperationJob(ctx context.Context, input CreateAgentOperationJobInput) (AgentConfigTask, error) {
	operation := strings.TrimSpace(input.Operation)
	if strings.TrimSpace(input.ServerID) == "" {
		return AgentConfigTask{}, fmt.Errorf("server id is required")
	}
	if !ValidVPNCoreOperation(operation) {
		return AgentConfigTask{}, fmt.Errorf("unsupported VPN Core operation %q", operation)
	}

	return scanAgentOperationTask(r.pool.QueryRow(ctx, `
		INSERT INTO agent_operation_jobs (server_id, agent_id, kind, operation)
		SELECT $1::uuid, a.id, $2, $3
		FROM agents a
		WHERE a.server_id = $1::uuid
		  AND a.status <> 'disabled'
		  AND a.capabilities ? 'vpnCoreServiceOperations'
		ORDER BY a.updated_at DESC
		LIMIT 1
		RETURNING
			id::text,
			server_id::text,
			agent_id::text,
			kind,
			operation,
			status,
			created_at,
			started_at
	`, input.ServerID, AgentTaskKindVPNCoreService, operation))
}

func (r *Repository) ClaimNextAgentOperationTask(ctx context.Context, tokenHash string) (*AgentConfigTask, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var agent Agent
	if agent, err = scanAgent(tx.QueryRow(ctx, agentSelect+` WHERE token_hash = $1`, tokenHash)); err != nil {
		return nil, err
	}

	task, err := scanAgentOperationTask(tx.QueryRow(ctx, `
		WITH next_job AS (
			SELECT id
			FROM agent_operation_jobs
			WHERE server_id = $1::uuid
			  AND agent_id = $2::uuid
			  AND status = 'pending'
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE agent_operation_jobs j
		SET
			status = 'in_progress',
			started_at = COALESCE(j.started_at, now()),
			updated_at = now()
		FROM next_job
		WHERE j.id = next_job.id
		RETURNING
			j.id::text,
			j.server_id::text,
			j.agent_id::text,
			j.kind,
			j.operation,
			j.status,
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

func (r *Repository) CompleteAgentOperationTask(ctx context.Context, input CompleteAgentOperationJobInput) error {
	if input.Status != AgentOperationJobStatusSucceeded && input.Status != AgentOperationJobStatusFailed {
		return fmt.Errorf("invalid operation job completion status %q", input.Status)
	}
	resultPayload := input.ResultPayload
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	payloadBytes, err := json.Marshal(resultPayload)
	if err != nil {
		return err
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE agent_operation_jobs j
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

func scanAgentOperationTask(row scanner) (AgentConfigTask, error) {
	var task AgentConfigTask
	var startedAt sql.NullTime
	err := row.Scan(
		&task.ID,
		&task.ServerID,
		&task.AgentID,
		&task.Kind,
		&task.Operation,
		&task.Status,
		&task.CreatedAt,
		&startedAt,
	)
	if err != nil {
		return AgentConfigTask{}, err
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	return task, nil
}
