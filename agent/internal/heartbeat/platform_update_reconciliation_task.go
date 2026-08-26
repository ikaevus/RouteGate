package heartbeat

import (
	"context"
	"fmt"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

const platformUpdateManagerStatusInProgress = "in_progress"

// processPlatformUpdateReconciliationTask remains the Runner entrypoint for
// platform_update tasks for compatibility with the existing task loop. E2i
// routes the fixed dispatch operation to the mutation handoff. Reconciliation
// never launches a worker or updater; it may only advance the bounded receipt
// monotonically after the fixed task-specific worker is proven absent.
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

	reconciliation, err := tasks.ReadPlatformUpdateReconciliation(ctx, task.ID, request.TargetVersion)
	if err != nil {
		// No completion is posted when receipt or fixed-worker evidence cannot be
		// read safely. The Manager job remains non-runnable and retries only
		// reconciliation on a future Agent poll.
		return fmt.Errorf("read platform update reconciliation: %w", err)
	}
	report := platformUpdateReconciliationReport(reconciliation)
	if platformUpdateReconciliationUsesFailureEnvelope(task.Status, reconciliation) {
		// A pre-dispatch terminal receipt reconciled while Manager still has only
		// in_progress evidence must use the failed transport envelope. Manager then
		// terminalizes without inventing dispatched_at. Once Manager already knows
		// mutation_dispatched, reconciliation always uses the successful transport
		// envelope so its existing dispatch provenance is preserved.
		if err := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, "", report); err != nil {
			return fmt.Errorf("report pre-dispatch platform update reconciliation: %w", err)
		}
	} else if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, report); err != nil {
		return fmt.Errorf("report platform update reconciliation: %w", err)
	}
	r.logger.Info("platform update reconciliation reported", "job_id", task.ID, "status", reconciliation.State)
	return nil
}

func platformUpdateReconciliationUsesFailureEnvelope(managerStatus string, reconciliation tasks.PlatformUpdateReconciliation) bool {
	return managerStatus == platformUpdateManagerStatusInProgress &&
		reconciliation.State == tasks.PlatformUpdateReconciliationFailed &&
		!reconciliation.MutationStarted
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
