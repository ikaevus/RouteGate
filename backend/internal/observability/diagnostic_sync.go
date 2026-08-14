package observability

import (
	"context"
	"encoding/json"
	"time"
)

type diagnosticTerminalJob struct {
	RunID       string
	ServerID    string
	ProfileKey  string
	JobStatus   string
	Payload     map[string]any
	CompletedAt time.Time
}

// SyncSemanticFromAgentJobs projects the generic Agent transport state into a
// bounded Observability diagnostic result. Raw Agent result fields never become
// RouteGate health meaning directly.
func (r *DiagnosticRepository) SyncSemanticFromAgentJobs(ctx context.Context) (int64, error) {
	var updated int64

	tag, err := r.pool.Exec(ctx, `
		UPDATE observability_diagnostic_runs d
		SET status='running',
		    started_at=COALESCE(d.started_at,j.started_at),
		    updated_at=now()
		FROM agent_operation_jobs j
		WHERE d.agent_operation_job_id=j.id
		  AND j.kind='diagnostic'
		  AND j.status='in_progress'
		  AND d.status='queued'
	`)
	if err != nil {
		return 0, err
	}
	updated += tag.RowsAffected()

	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.server_id::text, d.profile_key, j.status,
		       j.result_payload, COALESCE(j.completed_at, now())
		FROM observability_diagnostic_runs d
		JOIN agent_operation_jobs j ON j.id=d.agent_operation_job_id
		WHERE j.kind='diagnostic'
		  AND j.status IN ('succeeded','failed')
		  AND d.completed_at IS NULL
		ORDER BY d.requested_at, d.id
	`)
	if err != nil {
		return updated, err
	}
	defer rows.Close()

	jobs := make([]diagnosticTerminalJob, 0)
	for rows.Next() {
		var job diagnosticTerminalJob
		var raw []byte
		if err := rows.Scan(&job.RunID, &job.ServerID, &job.ProfileKey, &job.JobStatus, &raw, &job.CompletedAt); err != nil {
			return updated, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &job.Payload); err != nil {
				job.Payload = nil
			}
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return updated, err
	}

	for _, job := range jobs {
		if job.JobStatus == "failed" {
			tag, err := r.markDiagnosticTerminal(ctx, job, DiagnosticFailed, HealthUnknown, map[string]any{}, "diagnostic_execution_failed", "Diagnostic execution failed.", "retry_diagnostic", "diagnostic_execution_failed")
			if err != nil {
				return updated, err
			}
			updated += tag
			continue
		}

		result, safePayload, err := EvaluateDiagnosticPayload(job.ProfileKey, job.Payload, ResourceRef{Type: "server", ID: job.ServerID})
		if err != nil {
			tag, markErr := r.markDiagnosticTerminal(ctx, job, DiagnosticFailed, HealthUnknown, map[string]any{}, "diagnostic_result_invalid", "Diagnostic result could not be validated.", "retry_diagnostic", "diagnostic_result_invalid")
			if markErr != nil {
				return updated, markErr
			}
			updated += tag
			continue
		}
		tag, err := r.markDiagnosticTerminal(ctx, job, DiagnosticSucceeded, result.State, safePayload, result.ReasonCode, result.Summary, result.RecommendedAction, "")
		if err != nil {
			return updated, err
		}
		updated += tag
	}
	return updated, nil
}

func (r *DiagnosticRepository) markDiagnosticTerminal(
	ctx context.Context,
	job diagnosticTerminalJob,
	status DiagnosticStatus,
	state HealthState,
	payload map[string]any,
	reasonCode string,
	summary string,
	recommendedAction string,
	errorCode string,
) (int64, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE observability_diagnostic_runs
		SET status=$2,
		    state=$3,
		    result_payload=$4::jsonb,
		    reason_code=NULLIF($5,''),
		    summary=NULLIF($6,''),
		    recommended_action=NULLIF($7,''),
		    error_message=NULLIF($8,''),
		    completed_at=$9,
		    updated_at=now()
		WHERE id=$1::uuid AND completed_at IS NULL
	`, job.RunID, string(status), string(state), encoded, reasonCode, summary, recommendedAction, errorCode, job.CompletedAt.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
