package heartbeat

import (
	"context"
	"fmt"

	"github.com/ikaevus/routegate/agent/internal/diagnostics"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func (r *Runner) processDiagnosticTask(ctx context.Context, task tasks.ConfigTask) error {
	if task.EffectiveKind() != tasks.TaskKindDiagnostic {
		return fmt.Errorf("unsupported diagnostic task kind %q", task.EffectiveKind())
	}

	result, err := diagnostics.ExecuteWithOptions(task.Operation, diagnostics.Options{ManagerURL: r.cfg.ManagerURL})
	if err != nil {
		report := map[string]any{
			"schemaVersion": diagnostics.SchemaVersion,
			"profileKey":    task.Operation,
			"collectionStatus": "failed",
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("execute diagnostic: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, result); err != nil {
		return err
	}
	r.logger.Info("diagnostic task completed", "job_id", task.ID, "profile_key", task.Operation)
	return nil
}
