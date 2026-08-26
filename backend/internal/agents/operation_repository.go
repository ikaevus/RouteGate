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

	maxAgentOperationResultPayloadBytes = 64 * 1024
	maxAgentOperationErrorMessageBytes  = 2048
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
	case DiagnosticOperationHostOverview, DiagnosticOperationVPNCoreStatus, DiagnosticOperationManagerCertificate:
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
	case kind == AgentTaskKindVPNCoreInstall && ValidVPNCoreInstallationOperation(operation):
		return "vpnCoreInstallationOperations", nil
	case kind == AgentTaskKindDiagnostic && ValidDiagnosticOperation(operation):
		return "diagnosticProfiles", nil
	default:
		return "", fmt.Errorf("unsupported Agent operation kind %q operation %q", kind, operation)
	}
}

func platformUpdateTaskPayload(targetVersion string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"schemaVersion": 1,
		"targetVersion": targetVersion,
	})
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

	// An interrupted in_progress update and every mutation_dispatched update are
	// reconciliation-only. They can never return to the dispatch path. The
	// one-second touch interval prevents a tight polling loop while still
	// preserving durable read-only recovery after an acknowledgement loss.
	var reconcileTask AgentConfigTask
	var targetVersion string
	var startedAt sql.NullTime
	err = tx.QueryRow(ctx, `
		WITH next_job AS (
			SELECT id
			FROM agent_platform_update_jobs
			WHERE server_id = $1::uuid
			  AND agent_id = $2::uuid
			  AND status IN ('in_progress', 'mutation_dispatched')
			  AND updated_at <= now() - interval '1 second'
			ORDER BY
			  CASE status WHEN 'mutation_dispatched' THEN 0 ELSE 1 END,
			  updated_at ASC,
			  created_at ASC,
			  id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE agent_platform_update_jobs j
		SET updated_at = now()
		FROM next_job
		WHERE j.id = next_job.id
		RETURNING
			j.id::text,
			j.server_id::text,
			j.agent_id::text,
			j.target_version,
			j.status,
			j.created_at,
			j.started_at
	`, agent.ServerID, agent.ID).Scan(
		&reconcileTask.ID,
		&reconcileTask.ServerID,
		&reconcileTask.AgentID,
		&targetVersion,
		&reconcileTask.Status,
		&reconcileTask.CreatedAt,
		&startedAt,
	)
	if err == nil {
		payload, marshalErr := platformUpdateTaskPayload(targetVersion)
		if marshalErr != nil {
			return nil, marshalErr
		}
		reconcileTask.Kind = AgentTaskKindPlatformUpdate
		reconcileTask.Operation = PlatformUpdateOperationReconcile
		reconcileTask.RenderedConfig = payload
		if startedAt.Valid {
			reconcileTask.StartedAt = &startedAt.Time
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		return &reconcileTask, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// This is the one and only mutation-capable claim. pending is atomically
	// consumed into in_progress before the task leaves the Manager. No later
	// state is eligible for this query, so a transport loss cannot redispatch.
	var dispatchTask AgentConfigTask
	startedAt = sql.NullTime{}
	err = tx.QueryRow(ctx, `
		WITH next_job AS (
			SELECT id
			FROM agent_platform_update_jobs
			WHERE server_id = $1::uuid
			  AND agent_id = $2::uuid
			  AND status = 'pending'
			ORDER BY created_at ASC, id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE agent_platform_update_jobs j
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
			j.target_version,
			j.status,
			j.created_at,
			j.started_at
	`, agent.ServerID, agent.ID).Scan(
		&dispatchTask.ID,
		&dispatchTask.ServerID,
		&dispatchTask.AgentID,
		&targetVersion,
		&dispatchTask.Status,
		&dispatchTask.CreatedAt,
		&startedAt,
	)
	if err == nil {
		payload, marshalErr := platformUpdateTaskPayload(targetVersion)
		if marshalErr != nil {
			return nil, marshalErr
		}
		dispatchTask.Kind = AgentTaskKindPlatformUpdate
		dispatchTask.Operation = PlatformUpdateOperationDispatch
		dispatchTask.RenderedConfig = payload
		if startedAt.Valid {
			dispatchTask.StartedAt = &startedAt.Time
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		return &dispatchTask, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Do not let an ordinary mutating Agent operation run alongside an active
	// platform update merely because its reconciliation task is throttled or
	// another transaction currently owns the row lock.
	var platformUpdateActive bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM agent_platform_update_jobs
			WHERE server_id = $1::uuid
			  AND agent_id = $2::uuid
			  AND status IN ('pending', 'in_progress', 'mutation_dispatched')
		)
	`, agent.ServerID, agent.ID).Scan(&platformUpdateActive); err != nil {
		return nil, err
	}
	if platformUpdateActive {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, commitErr
		}
		return nil, nil
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
	if len(payloadBytes) > maxAgentOperationResultPayloadBytes {
		return "", fmt.Errorf("agent operation result payload exceeds %d bytes", maxAgentOperationResultPayloadBytes)
	}
	errorMessage := strings.TrimSpace(input.ErrorMessage)
	if len(errorMessage) > maxAgentOperationErrorMessageBytes {
		return "", fmt.Errorf("agent operation error message exceeds %d bytes", maxAgentOperationErrorMessageBytes)
	}

	// First handle the at-most-once dispatch state. The same in_progress row may
	// receive either the direct bounded dispatch acknowledgement or read-only
	// receipt evidence after an acknowledgement loss. It is never made runnable
	// again in either case.
	var targetVersion string
	err = r.pool.QueryRow(ctx, `
		SELECT j.target_version
		FROM agent_platform_update_jobs j
		JOIN agents a ON a.id = j.agent_id
		WHERE j.id = $1::uuid
		  AND a.token_hash = $2
		  AND j.status = 'in_progress'
	`, input.JobID, input.TokenHash).Scan(&targetVersion)
	if err == nil {
		return r.completeInProgressPlatformUpdate(ctx, input, targetVersion, errorMessage)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	// A platform_update result in mutation_dispatched is a read-only receipt
	// projection, never a mutation outcome by itself. Only a successful transport
	// response carrying matching strict evidence may reconcile the lifecycle.
	err = r.pool.QueryRow(ctx, `
		SELECT j.target_version
		FROM agent_platform_update_jobs j
		JOIN agents a ON a.id = j.agent_id
		WHERE j.id = $1::uuid
		  AND a.token_hash = $2
		  AND j.status = 'mutation_dispatched'
	`, input.JobID, input.TokenHash).Scan(&targetVersion)
	if err == nil {
		if input.Status != AgentOperationJobStatusSucceeded || errorMessage != "" {
			return "", fmt.Errorf("platform update reconciliation transport must succeed before evidence is accepted")
		}
		evidence, decodeErr := DecodePlatformUpdateReconciliationEvidence(resultPayload)
		if decodeErr != nil {
			return "", decodeErr
		}
		nextStatus, reconcileErr := ReconcilePlatformUpdateEvidence(input.JobID, targetVersion, evidence)
		if reconcileErr != nil {
			return "", reconcileErr
		}
		if nextStatus == AgentOperationJobStatusMutationDispatched {
			commandTag, updateErr := r.pool.Exec(ctx, `
				UPDATE agent_platform_update_jobs
				SET updated_at = now()
				WHERE id = $1::uuid
				  AND agent_id = (SELECT id FROM agents WHERE token_hash = $2)
				  AND status = 'mutation_dispatched'
			`, input.JobID, input.TokenHash)
			if updateErr != nil {
				return "", updateErr
			}
			if commandTag.RowsAffected() != 1 {
				return "", pgx.ErrNoRows
			}
			return AgentTaskKindPlatformUpdate, nil
		}
		if !ValidPlatformUpdateTransition(AgentOperationJobStatusMutationDispatched, nextStatus) {
			return "", fmt.Errorf("invalid platform update reconciliation transition")
		}
		errorCode := ""
		if nextStatus == AgentOperationJobStatusFailed || nextStatus == AgentOperationJobStatusOutcomeUnknown {
			errorCode = evidence.Code
		}
		commandTag, updateErr := r.pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET
				status = $3,
				error_code = NULLIF($4, ''),
				completed_at = now(),
				updated_at = now()
			WHERE id = $1::uuid
			  AND agent_id = (SELECT id FROM agents WHERE token_hash = $2)
			  AND status = 'mutation_dispatched'
		`, input.JobID, input.TokenHash, nextStatus, errorCode)
		if updateErr != nil {
			return "", updateErr
		}
		if commandTag.RowsAffected() != 1 {
			return "", pgx.ErrNoRows
		}
		return AgentTaskKindPlatformUpdate, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
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
	`, input.JobID, input.TokenHash, input.Status, payloadBytes, errorMessage).Scan(&kind)
	if err != nil {
		return "", err
	}
	return kind, nil
}

func (r *Repository) completeInProgressPlatformUpdate(ctx context.Context, input CompleteAgentOperationJobInput, targetVersion, errorMessage string) (string, error) {
	evidence, err := DecodePlatformUpdateReconciliationEvidence(input.ResultPayload)
	if err != nil {
		return "", err
	}
	if evidence.TaskID != input.JobID || evidence.TargetVersion != targetVersion {
		return "", fmt.Errorf("platform update dispatch identity mismatch")
	}

	if input.Status == AgentOperationJobStatusFailed {
		// Deterministic pre-dispatch failure is the only failed transport envelope
		// allowed while the Manager row is in_progress. Raw error text is rejected.
		if errorMessage != "" {
			return "", fmt.Errorf("platform update pre-dispatch failure must not contain raw error text")
		}
		nextStatus, reconcileErr := ReconcilePlatformUpdateEvidence(input.JobID, targetVersion, evidence)
		if reconcileErr != nil {
			return "", reconcileErr
		}
		if nextStatus != AgentOperationJobStatusFailed {
			return "", fmt.Errorf("platform update failed dispatch envelope requires failed evidence")
		}
		commandTag, updateErr := r.pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET
				status = 'failed',
				error_code = $3,
				completed_at = now(),
				updated_at = now()
			WHERE id = $1::uuid
			  AND agent_id = (SELECT id FROM agents WHERE token_hash = $2)
			  AND status = 'in_progress'
			  AND dispatched_at IS NULL
		`, input.JobID, input.TokenHash, evidence.Code)
		if updateErr != nil {
			return "", updateErr
		}
		if commandTag.RowsAffected() != 1 {
			return "", pgx.ErrNoRows
		}
		return AgentTaskKindPlatformUpdate, nil
	}

	if errorMessage != "" {
		return "", fmt.Errorf("platform update dispatch acknowledgement must not contain raw error text")
	}
	if evidence.Status == PlatformUpdateReceiptStatusMutationDispatched {
		if evidence.Code != "" {
			return "", fmt.Errorf("mutation_dispatched evidence must not carry a code")
		}
		commandTag, updateErr := r.pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET
				status = 'mutation_dispatched',
				dispatched_at = COALESCE(dispatched_at, now()),
				updated_at = now()
			WHERE id = $1::uuid
			  AND agent_id = (SELECT id FROM agents WHERE token_hash = $2)
			  AND status = 'in_progress'
		`, input.JobID, input.TokenHash)
		if updateErr != nil {
			return "", updateErr
		}
		if commandTag.RowsAffected() != 1 {
			return "", pgx.ErrNoRows
		}
		return AgentTaskKindPlatformUpdate, nil
	}

	// Successful transport while Manager is still in_progress means this was a
	// reconciliation task after a lost acknowledgement. Matching receipt evidence
	// proves durable Agent handoff, so even pending evidence is promoted to the
	// non-runnable mutation_dispatched state. Terminal receipt evidence may
	// terminalize directly while recording dispatched_at to preserve provenance.
	nextStatus, reconcileErr := ReconcilePlatformUpdateEvidence(input.JobID, targetVersion, evidence)
	if reconcileErr != nil {
		return "", reconcileErr
	}
	if nextStatus == AgentOperationJobStatusMutationDispatched {
		commandTag, updateErr := r.pool.Exec(ctx, `
			UPDATE agent_platform_update_jobs
			SET
				status = 'mutation_dispatched',
				dispatched_at = COALESCE(dispatched_at, now()),
				updated_at = now()
			WHERE id = $1::uuid
			  AND agent_id = (SELECT id FROM agents WHERE token_hash = $2)
			  AND status = 'in_progress'
		`, input.JobID, input.TokenHash)
		if updateErr != nil {
			return "", updateErr
		}
		if commandTag.RowsAffected() != 1 {
			return "", pgx.ErrNoRows
		}
		return AgentTaskKindPlatformUpdate, nil
	}
	if !PlatformUpdateStatusIsTerminal(nextStatus) {
		return "", fmt.Errorf("invalid in-progress platform update reconciliation result")
	}
	errorCode := ""
	if nextStatus == AgentOperationJobStatusFailed || nextStatus == AgentOperationJobStatusOutcomeUnknown {
		errorCode = evidence.Code
	}
	commandTag, updateErr := r.pool.Exec(ctx, `
		UPDATE agent_platform_update_jobs
		SET
			status = $3,
			dispatched_at = COALESCE(dispatched_at, now()),
			error_code = NULLIF($4, ''),
			completed_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		  AND agent_id = (SELECT id FROM agents WHERE token_hash = $2)
		  AND status = 'in_progress'
	`, input.JobID, input.TokenHash, nextStatus, errorCode)
	if updateErr != nil {
		return "", updateErr
	}
	if commandTag.RowsAffected() != 1 {
		return "", pgx.ErrNoRows
	}
	return AgentTaskKindPlatformUpdate, nil
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
