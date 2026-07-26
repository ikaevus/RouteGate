package agents

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (r *Repository) GetAgentOperationJob(ctx context.Context, serverID, jobID string) (AgentConfigTask, error) {
	var task AgentConfigTask
	var resultPayload []byte
	var errorMessage sql.NullString
	var updatedAt, startedAt, completedAt sql.NullTime
	err := r.pool.QueryRow(ctx, `
		SELECT
			id::text,
			server_id::text,
			COALESCE(agent_id::text, ''),
			kind,
			operation,
			status,
			result_payload,
			error_message,
			created_at,
			updated_at,
			started_at,
			completed_at
		FROM agent_operation_jobs
		WHERE id = $1::uuid AND server_id = $2::uuid
	`, jobID, serverID).Scan(
		&task.ID,
		&task.ServerID,
		&task.AgentID,
		&task.Kind,
		&task.Operation,
		&task.Status,
		&resultPayload,
		&errorMessage,
		&task.CreatedAt,
		&updatedAt,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		return AgentConfigTask{}, err
	}
	if len(resultPayload) > 0 {
		if err := json.Unmarshal(resultPayload, &task.ResultPayload); err != nil {
			return AgentConfigTask{}, err
		}
	}
	if errorMessage.Valid {
		task.ErrorMessage = errorMessage.String
	}
	if updatedAt.Valid {
		task.UpdatedAt = &updatedAt.Time
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	return task, nil
}
