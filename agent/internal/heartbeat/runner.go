package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ikaevus/routegate/agent/internal/client"
	"github.com/ikaevus/routegate/agent/internal/config"
	"github.com/ikaevus/routegate/agent/internal/systeminfo"
	"github.com/ikaevus/routegate/agent/internal/tasks"
	"github.com/ikaevus/routegate/agent/internal/traffic"
)

type Runner struct {
	cfg               config.Config
	configPath        string
	client            *client.Client
	logger            *slog.Logger
	trafficCollector  traffic.Collector
	trafficTracker    *traffic.DeltaTracker
	lastTrafficReport time.Time
}

func NewRunner(cfg config.Config, configPath string, logger *slog.Logger) *Runner {
	runner := &Runner{
		cfg:              cfg,
		configPath:       configPath,
		client:           client.New(cfg.ManagerURL),
		logger:           logger,
		trafficCollector: traffic.NoopCollector{},
		trafficTracker:   traffic.NewDeltaTracker(),
	}
	if cfg.TrafficCollectionEnabled {
		runner.trafficCollector = traffic.NewFileCollector(cfg.TrafficUsageFilePath)
	}
	return runner
}

func (r *Runner) Run(ctx context.Context, once bool) error {
	if err := r.ensureRegistered(ctx); err != nil {
		return err
	}
	if once {
		if err := r.sendHeartbeat(ctx); err != nil {
			return err
		}
		if err := r.processNextTask(ctx); err != nil {
			return err
		}
		if err := r.reportTrafficUsage(ctx); err != nil {
			r.logger.Warn("report traffic usage failed", "error", err)
		}
		return nil
	}
	if err := r.sendHeartbeat(ctx); err != nil {
		r.logger.Warn("heartbeat failed", "error", err)
	}
	if err := r.processNextTask(ctx); err != nil {
		r.logger.Warn("process agent task failed", "error", err)
	}
	if err := r.reportTrafficUsage(ctx); err != nil {
		r.logger.Warn("report traffic usage failed", "error", err)
	}
	ticker := time.NewTicker(r.cfg.HeartbeatInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.sendHeartbeat(ctx); err != nil {
				r.logger.Warn("heartbeat failed", "error", err)
			}
			if err := r.processNextTask(ctx); err != nil {
				r.logger.Warn("process agent task failed", "error", err)
			}
			if err := r.reportTrafficUsage(ctx); err != nil {
				r.logger.Warn("report traffic usage failed", "error", err)
			}
		}
	}
}

func (r *Runner) ensureRegistered(ctx context.Context) error {
	if r.cfg.HasAgentCredentials() {
		return nil
	}
	if !r.cfg.HasRegistrationToken() {
		return errors.New("agent_token or registration_token is required")
	}
	info := systeminfo.Collect()
	res, err := r.client.Register(ctx, r.cfg, info)
	if err != nil {
		return err
	}
	r.cfg.AgentID = res.AgentID
	r.cfg.ServerID = res.ServerID
	r.cfg.AgentToken = res.AgentToken
	r.cfg.RegistrationToken = ""
	if err := r.cfg.Save(r.configPath); err != nil {
		return err
	}
	r.logger.Info("agent registered", "agent_id", r.cfg.AgentID, "server_id", r.cfg.ServerID)
	return nil
}

func (r *Runner) sendHeartbeat(ctx context.Context) error {
	info := systeminfo.Collect()
	res, err := r.client.Heartbeat(ctx, r.cfg.AgentToken, info)
	if err != nil {
		return err
	}
	r.logger.Info("heartbeat accepted", "agent_id", res.AgentID, "server_id", res.ServerID, "server_status", res.ServerStatus)
	return nil
}

func (r *Runner) reportTrafficUsage(ctx context.Context) error {
	if !r.cfg.TrafficCollectionEnabled {
		return nil
	}
	now := time.Now().UTC()
	if !r.lastTrafficReport.IsZero() && now.Sub(r.lastTrafficReport) < r.cfg.TrafficCollectionInterval() {
		return nil
	}
	r.lastTrafficReport = now

	snapshots, err := r.trafficCollector.Collect(ctx)
	if err != nil {
		return err
	}
	usageEvents := r.trafficTracker.BuildUsageEvents(snapshots)
	if len(usageEvents) == 0 {
		r.logger.Debug("no traffic usage delta available", "snapshots", len(snapshots))
		return nil
	}

	res, err := r.client.ReportTrafficUsage(ctx, r.cfg.AgentToken, usageEvents)
	if err != nil {
		return err
	}
	r.logger.Info("traffic usage report accepted", "agent_id", res.AgentID, "server_id", res.ServerID, "accepted", res.Accepted)
	return nil
}

func (r *Runner) processNextTask(ctx context.Context) error {
	task, err := r.client.NextTask(ctx, r.cfg.AgentToken)
	if err != nil {
		return err
	}
	if task == nil {
		r.logger.Debug("no agent task available")
		return nil
	}

	switch task.EffectiveKind() {
	case tasks.TaskKindVPNCoreService:
		return r.processVPNCoreServiceTask(ctx, *task)
	case tasks.TaskKindVPNCoreInstall:
		return r.processVPNCoreInstallTask(ctx, *task)
	case tasks.TaskKindDiagnostic:
		return r.processDiagnosticTask(ctx, *task)
	case tasks.TaskKindConfigApply:
		// Continue through the existing config deployment workflow.
	default:
		err := fmt.Errorf("unsupported agent task kind %q", task.Kind)
		report := map[string]any{"kind": task.Kind, "status": "rejected"}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("reject unsupported task kind: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	stageResult, err := tasks.NewStager(r.cfg.ConfigStagingDir).Stage(*task)
	if err != nil {
		report := map[string]any{
			"stage":           "failed",
			"validate":        "skipped",
			"apply":           "skipped",
			"configVersionId": task.ConfigVersionID,
			"configHash":      task.ConfigHash,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("stage config task failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	validationResult, err := tasks.NewValidator(r.cfg.SingBoxPath).Check(ctx, stageResult.StagedPath)
	if err != nil {
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "failed",
			"apply":           "skipped",
			"stagedPath":      stageResult.StagedPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      stageResult.ConfigHash,
			"command":         validationResult.Command,
			"output":          validationResult.Output,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("validate staged config failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	applyResult, err := tasks.NewApplier(r.cfg.ActiveConfigPath, r.cfg.ConfigBackupDir).Apply(stageResult.StagedPath, stageResult.ConfigVersionID)
	if err != nil {
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "succeeded",
			"apply":           "failed",
			"stagedPath":      stageResult.StagedPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      task.ConfigHash,
			"command":         validationResult.Command,
			"output":          validationResult.Output,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("apply staged config failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	if !r.cfg.ServiceControlEnabled {
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "succeeded",
			"apply":           "succeeded",
			"restart":         "skipped_service_control_disabled",
			"healthcheck":     "skipped_service_control_disabled",
			"rollback":        "skipped",
			"stagedPath":      stageResult.StagedPath,
			"activePath":      applyResult.ActivePath,
			"backupPath":      applyResult.BackupPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      task.ConfigHash,
			"command":         validationResult.Command,
			"output":          validationResult.Output,
		}
		if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, report); err != nil {
			return err
		}
		r.logger.Info("config task staged, validated and applied; service control skipped", "job_id", task.ID, "config_version_id", stageResult.ConfigVersionID, "staged_path", stageResult.StagedPath, "active_path", applyResult.ActivePath)
		return nil
	}

	service := tasks.NewServiceController(r.cfg.SingBoxServiceName)
	restartResult, err := service.Restart(ctx)
	if err != nil {
		rollbackStatus := r.rollbackAppliedConfig(applyResult)
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "succeeded",
			"apply":           "succeeded",
			"restart":         "failed",
			"healthcheck":     "skipped",
			"rollback":        rollbackStatus,
			"stagedPath":      stageResult.StagedPath,
			"activePath":      applyResult.ActivePath,
			"backupPath":      applyResult.BackupPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      task.ConfigHash,
			"command":         restartResult.Command,
			"output":          restartResult.Output,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("restart sing-box failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	healthResult, err := service.IsActive(ctx)
	if err != nil {
		rollbackStatus := r.rollbackAppliedConfig(applyResult)
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "succeeded",
			"apply":           "succeeded",
			"restart":         "succeeded",
			"healthcheck":     "failed",
			"rollback":        rollbackStatus,
			"stagedPath":      stageResult.StagedPath,
			"activePath":      applyResult.ActivePath,
			"backupPath":      applyResult.BackupPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      task.ConfigHash,
			"command":         healthResult.Command,
			"output":          healthResult.Output,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("sing-box healthcheck failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	persistenceResult, err := service.IsEnabled(ctx)
	if err != nil {
		rollbackStatus := r.rollbackAppliedConfig(applyResult)
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "succeeded",
			"apply":           "succeeded",
			"restart":         "succeeded",
			"healthcheck":     "failed",
			"rollback":        rollbackStatus,
			"stagedPath":      stageResult.StagedPath,
			"activePath":      applyResult.ActivePath,
			"backupPath":      applyResult.BackupPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      task.ConfigHash,
			"command":         persistenceResult.Command,
			"output":          persistenceResult.Output,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("sing-box persistence check failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	listenerResult, err := tasks.CheckVLESSListener(ctx, applyResult.ActivePath)
	if err != nil {
		rollbackStatus := r.rollbackAppliedConfig(applyResult)
		report := map[string]any{
			"stage":           "succeeded",
			"validate":        "succeeded",
			"apply":           "succeeded",
			"restart":         "succeeded",
			"healthcheck":     "failed",
			"rollback":        rollbackStatus,
			"stagedPath":      stageResult.StagedPath,
			"activePath":      applyResult.ActivePath,
			"backupPath":      applyResult.BackupPath,
			"configVersionId": stageResult.ConfigVersionID,
			"configHash":      task.ConfigHash,
			"listenerPort":    listenerResult.Port,
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), report); completeErr != nil {
			return fmt.Errorf("sing-box listener healthcheck failed: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	report := map[string]any{
		"stage":              "succeeded",
		"validate":           "succeeded",
		"apply":              "succeeded",
		"restart":            "succeeded",
		"healthcheck":        "succeeded",
		"rollback":           "skipped",
		"stagedPath":         stageResult.StagedPath,
		"activePath":         applyResult.ActivePath,
		"backupPath":         applyResult.BackupPath,
		"configVersionId":    stageResult.ConfigVersionID,
		"configHash":         task.ConfigHash,
		"command":            validationResult.Command,
		"output":             validationResult.Output,
		"restartCommand":     restartResult.Command,
		"healthCommand":      healthResult.Command,
		"persistenceCommand": persistenceResult.Command,
		"listenerAddress":    listenerResult.Address,
		"listenerPort":       listenerResult.Port,
	}
	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, report); err != nil {
		return err
	}
	r.logger.Info("config task staged, validated and applied", "job_id", task.ID, "config_version_id", task.ConfigVersionID, "staged_path", stageResult.StagedPath, "active_path", applyResult.ActivePath, "listener_port", listenerResult.Port)
	return nil
}

func (r *Runner) rollbackAppliedConfig(result tasks.ApplyResult) string {
	if result.BackupPath == "" {
		return "skipped_no_backup"
	}
	if err := tasks.NewApplier(r.cfg.ActiveConfigPath, r.cfg.ConfigBackupDir).Rollback(result.BackupPath); err != nil {
		r.logger.Warn("rollback active config failed", "error", err, "backup_path", result.BackupPath, "active_path", result.ActivePath)
		return "failed"
	}
	r.logger.Warn("rolled back active config", "backup_path", result.BackupPath, "active_path", result.ActivePath)
	return "succeeded"
}
