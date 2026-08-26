package heartbeat

import (
	"context"
	"fmt"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

// processPlatformUpdateReconciliationTask remains the Runner entrypoint for
// platform_update tasks for compatibility with the existing task loop. E2i
// routes the new fixed dispatch operation to the mutation handoff; reconcile
// remains strictly read-only.
func (r *Runner) processPlatformUpdateReconciliationTask(ctx context.Context, task tasks.ConfigTask) error {
	if task.EffectiveKind() != tasks.TaskKindPlatformUpdate {
		return fmt.Errorf("unsupported platform update task kind %q", task.EffectiveKind())
	}
	if task.Operation == tasks.PlatformUpdateOperationDispatch {
		return r.processPlatformUpdateDispatchTask(ctx, task)
	}
	if task.Operation != tasks.PlatformUpdateOperationReconcile {
		return fmt.Errorf("unsupported platform update operation %q", task.Operation)
	}
	request, err := tasks.DecodePlatformUpdateRequest(task.RenderedConfig)
	if err != nil {
		return fmt.Errorf("decode platform update reconciliation request: %w", err)
	}

	reconciliation, err := tasks.ReadPlatformUpdateReconciliation(task.ID, request.TargetVersion)
	if err != nil {
		// No completion is posted when receipt evidence cannot be read safely.
		// The Manager job remains non-runnable and will retry only the read-only
		// reconciliation task on a future Agent poll.
		return fmt.Errorf("read platform update reconciliation: %w", err)
	}
	report := platformUpdateReconciliationReport(reconciliation)
	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, report); err != nil {
		return fmt.Errorf("report platform update reconciliation: %w", err)
	}
	r.logger.Info("platform update reconciliation reported", "job_id", task.ID, "status", reconciliation.State)
	return nil
}

func platformUpdateReconciliationReport(reconciliation tasks.PlatformUpdateReconciliation) map[string]any {
	report := map[string]any{
		"taskId":        reconciliation.TaskID,
		"targetVersion": reconciliation.TargetVersion,
		"status":        string(reconciliation.State),
	}
	if reconciliation.Code != "" {
		report["code"] = reconciliation.Code
	}
	return report
}
