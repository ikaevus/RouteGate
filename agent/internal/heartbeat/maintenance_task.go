package heartbeat

import (
	"context"
	"fmt"

	"github.com/ikaevus/routegate/agent/internal/runtimecleanup"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func (r *Runner) processMaintenanceTask(ctx context.Context, task tasks.ConfigTask) error {
	request, err := runtimecleanup.DecodeRequest(task.RenderedConfig, task.Operation)
	if err != nil {
		result := map[string]any{"schemaVersion": runtimecleanup.SchemaVersion, "operation": task.Operation, "status": "rejected"}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, "invalid typed maintenance request", result); completeErr != nil {
			return fmt.Errorf("reject maintenance task: %v; report failure: %w", err, completeErr)
		}
		return err
	}
	result, err := r.runtimeCleaner.Execute(task.Operation, request)
	resultPayload := map[string]any{
		"schemaVersion":  result.SchemaVersion,
		"operation":      result.Operation,
		"candidateCount": result.CandidateCount,
		"estimatedBytes": result.EstimatedBytes,
		"deletedCount":   result.DeletedCount,
		"reclaimedBytes": result.ReclaimedBytes,
		"remainingCount": result.RemainingCount,
		"candidates":     result.Candidates,
	}
	if err != nil {
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, "maintenance operation failed safely", resultPayload); completeErr != nil {
			return fmt.Errorf("execute maintenance task: %v; report failure: %w", err, completeErr)
		}
		return err
	}
	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, resultPayload); err != nil {
		return fmt.Errorf("report maintenance task result: %w", err)
	}
	r.logger.Info("maintenance task completed", "job_id", task.ID, "operation", task.Operation)
	return nil
}
