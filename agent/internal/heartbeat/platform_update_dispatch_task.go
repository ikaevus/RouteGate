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
		// A malformed privileged task must never reach staging or the worker. We
		// intentionally do not invent terminal evidence for a request whose
		// version identity could not be decoded safely.
		return fmt.Errorf("decode platform update dispatch request: %w", err)
	}

	_, err = tasks.PrepareAndStartDetachedPlatformUpdate(ctx, task.ID, request)
	if err != nil {
		if errors.Is(err, tasks.ErrPlatformUpdateDispatchAmbiguous) {
			// Once launch acceptance is ambiguous, never post a deterministic
			// failure. Manager keeps in_progress non-runnable and a later poll is
			// read-only receipt reconciliation.
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
		// Lost acknowledgement after systemd acceptance is post-dispatch
		// ambiguity. The Manager row remains in_progress and is reconciliation-
		// only on the next poll; it is never returned as dispatch again.
		return fmt.Errorf("report platform update mutation dispatch: %w", err)
	}
	r.logger.Info("platform update mutation dispatched", "job_id", task.ID, "target_version", request.TargetVersion)
	return nil
}
