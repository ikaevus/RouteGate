package heartbeat

import (
	"context"
	"errors"
	"fmt"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

const (
	platformUpdateDispatchStatusMutationDispatched = "mutation_dispatched"
	platformUpdateDispatchStatusFailed             = "failed"
	platformUpdateDispatchCodeFailed               = "pre_dispatch_failed"
)

func (r *Runner) processPlatformUpdateTask(ctx context.Context, task tasks.ConfigTask) error {
	if task.EffectiveKind() != tasks.TaskKindPlatformUpdate {
		return fmt.Errorf("unsupported platform update task kind %q", task.EffectiveKind())
	}
	switch task.Operation {
	case tasks.PlatformUpdateOperationDispatch:
		return r.processPlatformUpdateDispatchTask(ctx, task)
	case tasks.PlatformUpdateOperationReconcile:
		return r.processPlatformUpdateReconciliationTask(ctx, task)
	default:
		return fmt.Errorf("unsupported platform update operation %q", task.Operation)
	}
}

func (r *Runner) processPlatformUpdateDispatchTask(ctx context.Context, task tasks.ConfigTask) error {
	request, err := tasks.DecodePlatformUpdateRequest(task.RenderedConfig)
	if err != nil {
		return fmt.Errorf("decode platform update dispatch request: %w", err)
	}

	// Heartbeat readiness is only discovery evidence and may be stale. Repeat
	// the fixed executable + full parent-chain validation before staging. If it
	// fails, persist the deterministic pre-dispatch outcome before attempting to
	// acknowledge it to Manager so an acknowledgement loss remains reconcilable.
	if !tasks.PlatformUpdateRuntimeReady() {
		if receiptErr := tasks.RecordPlatformUpdatePreDispatchFailure(task.ID, request.TargetVersion, "runtime_not_ready"); receiptErr != nil {
			return fmt.Errorf("platform update runtime is not safely ready; persist bounded failure: %w", receiptErr)
		}
		report := map[string]any{
			"taskId":        task.ID,
			"targetVersion": request.TargetVersion,
			"status":        platformUpdateDispatchStatusFailed,
			"code":          platformUpdateDispatchCodeFailed,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, "", report); completeErr != nil {
			return fmt.Errorf("platform update runtime is not safely ready; report bounded failure: %w", completeErr)
		}
		return fmt.Errorf("platform update runtime is not safely ready")
	}

	_, err = tasks.PrepareAndStartDetachedPlatformUpdate(ctx, task.ID, request)
	if err != nil {
		if errors.Is(err, tasks.ErrPlatformUpdateDispatchAmbiguous) {
			return fmt.Errorf("platform update dispatch acknowledgement is ambiguous: %w", err)
		}
		report := map[string]any{
			"taskId":        task.ID,
			"targetVersion": request.TargetVersion,
			"status":        platformUpdateDispatchStatusFailed,
			"code":          platformUpdateDispatchCodeFailed,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, "", report); completeErr != nil {
			return fmt.Errorf("platform update pre-dispatch failed: %v; report bounded failure: %w", err, completeErr)
		}
		return fmt.Errorf("platform update pre-dispatch failed: %w", err)
	}

	report := map[string]any{
		"taskId":        task.ID,
		"targetVersion": request.TargetVersion,
		"status":        platformUpdateDispatchStatusMutationDispatched,
	}
	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, report); err != nil {
		return fmt.Errorf("report platform update mutation dispatch: %w", err)
	}
	r.logger.Info("platform update mutation dispatched", "job_id", task.ID, "target_version", request.TargetVersion)
	return nil
}
