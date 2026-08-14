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
	Kind      string
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

func ValidDiagnosticOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case DiagnosticOperationHostOverview, DiagnosticOperationVPNCoreStatus:
		return true
	default:
		return false
	}
}

func (r *Repository) CreateAgentOperationJob(ctx context.Context, input CreateAgentOperationJobInput) (AgentConfigTask, error) {
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = AgentTaskKindVPNCoreService
	}
	operation := strings.TrimSpace(input.Operation)
	if strings.TrimSpace(input.ServerID) == "" {
		return AgentConfigTask{}, fmt.Errorf("server id is required")
	}
	capability, err := operationCapability(kind, operation)
	if err != nil {
		return AgentConfigTask{}, err
	}
	capabilityJSON, err := json.Marshal(map[string]any{capability: []string{operation}})
	if err != nil {
		return AgentConfigTask{}, err
	}

	return scanAgentOperationTask(r.pool.QueryRow(ctx, `
		INSERT INTO agent_operation_jobs (server_id, agent_id, kind, operation)
		SELECT $1::uuid, a.id, $2, $3
		FROM agents a
		WHERE a.server_id = $1::uuid
		  AND a.status <> 'disabled'
		  AND a.capabilities @> $4::jsonb
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
	`, input.ServerID, kind, operation, capabilityJSON))
}

func operationCapability(kind, operation string) (string, error) {
	switch {
	case kind == AgentTaskKindVPNCoreService && ValidVPNCoreOperation(operation):
		return "vpnCoreServiceOperations", nil
	case kind == AgentTaskKindVPNCoreInstall && operation == VPNCoreOperationInstallSingBox:
		return "vpnCoreInstallationOperations", nil
	case kind == AgentTaskKindDiagnostic && ValidDiagnosticOperation(operation):
		return "diagnosticProfiles", nil
	default:
		return "", fmt.Errorf("unsupported Agent operation kind %q operation %q", kind, operation)
	}
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

func (r *Repository) CompleteAgentOperationTask(ctx context.Context, input CompleteAgentOperationJobInput) (string, error) {
	if input.Status != AgentOperationJobStatusSucceeded && input.Status != AgentOperationJobStatusFailed {
		return "", fmt.Errorf("invalid operation job completion status %q", input.Status)
	}
	resultPayload := input.ResultPayload
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	payloadBytes, err := json.Marshal(resultPayload)
	if err != nil {
		return "", err
	}

	var kind string
	err = r.pool.QueryRow(ctx, `
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
		RETURNING j.kind
	`, input.JobID, input.TokenHash, input.Status, payloadBytes, strings.TrimSpace(input.ErrorMessage)).Scan(&kind)
	if err != nil {
		return "", err
	}
	return kind, nil
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
