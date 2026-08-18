package heartbeat

import (
	"context"
	"fmt"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func (r *Runner) processVPNCoreServiceTask(ctx context.Context, task tasks.ConfigTask) error {
	if !r.cfg.ServiceControlEnabled {
		err := fmt.Errorf("VPN Core service control is disabled by Agent configuration")
		report := map[string]any{
			"kind":      tasks.TaskKindVPNCoreService,
			"operation": task.Operation,
			"status":    "rejected_service_control_disabled",
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("reject VPN Core service task: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	result, err := r.vpnCoreAdapter.ExecuteServiceTask(ctx, task)
	report := map[string]any{
		"kind":      result.Kind,
		"operation": result.Operation,
		"service":   result.Service,
		"command":   result.Command,
		"output":    result.Output,
	}
	if err != nil {
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("execute VPN Core service task failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}
	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, report); err != nil {
		return err
	}
	r.logger.Info("VPN Core service task completed", "job_id", task.ID, "operation", result.Operation, "service", result.Service)
	return nil
}
