package heartbeat

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
	"github.com/ikaevus/routegate/agent/internal/vpncoreinstall"
)

const installedServiceEnableTimeout = 10 * time.Second

func (r *Runner) processVPNCoreInstallTask(ctx context.Context, task tasks.ConfigTask) error {
	if validationErr := tasks.ValidateVPNCoreInstallTask(task); validationErr != nil {
		err := fmt.Errorf("unsupported_installation_task")
		report := vpncoreinstall.Report{
			Kind:      tasks.TaskKindVPNCoreInstall,
			Operation: task.Operation,
			Status:    "failed",
			Stages: []vpncoreinstall.StageResult{{
				Stage: "detect_platform", Status: "failed", Code: "unsupported_installation_task",
			}},
		}
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), reportMap(report)); completeErr != nil {
			return fmt.Errorf("reject VPN Core installation task: %v; report failure: %w", err, completeErr)
		}
		return err
	}

	report, err := vpncoreinstall.Execute(ctx, task.Operation)
	if err != nil {
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), reportMap(report)); completeErr != nil {
			return fmt.Errorf("execute VPN Core installation task: %v; report failure: %w", err, completeErr)
		}
		return err
	}
	if err := ensureInstalledServicePersistent(ctx, &report); err != nil {
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), reportMap(report)); completeErr != nil {
			return fmt.Errorf("enable installed VPN runtime service: %v; report failure: %w", err, completeErr)
		}
		return err
	}
	if err := r.client.CompleteTaskSucceeded(ctx, r.cfg.AgentToken, task.ID, reportMap(report)); err != nil {
		return err
	}
	r.logger.Info("VPN Core installation task completed",
		"job_id", task.ID,
		"operation", report.Operation,
		"binary_path", report.BinaryPath,
		"service", report.ServiceName,
	)
	return nil
}

// Runtime installers deliberately leave newly-created services inactive until a
// validated RouteGate configuration is present. They still need to be enabled
// for reboot persistence because the normal config-apply transaction verifies
// both runtime health and systemd enablement before it can commit the protocol
// as active.
func ensureInstalledServicePersistent(ctx context.Context, report *vpncoreinstall.Report) error {
	serviceName := strings.TrimSpace(report.ServiceName)
	if serviceName == "" {
		return nil
	}
	enableCtx, cancel := context.WithTimeout(ctx, installedServiceEnableTimeout)
	defer cancel()
	if err := exec.CommandContext(enableCtx, "systemctl", "enable", "--quiet", serviceName).Run(); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{
			Stage: "enable_service", Status: "failed", Code: "service_persistence_enable_failed",
		})
		return fmt.Errorf("service_persistence_enable_failed")
	}
	report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "enable_service", Status: "succeeded"})
	return nil
}

func reportMap(report vpncoreinstall.Report) map[string]any {
	stages := make([]map[string]any, 0, len(report.Stages))
	for _, stage := range report.Stages {
		item := map[string]any{"stage": stage.Stage, "status": stage.Status}
		if stage.Code != "" {
			item["code"] = stage.Code
		}
		stages = append(stages, item)
	}
	return map[string]any{
		"kind":           report.Kind,
		"operation":      report.Operation,
		"status":         report.Status,
		"platform":       map[string]any{"id": report.Platform.ID, "version": report.Platform.Version, "architecture": report.Platform.Architecture},
		"singBoxVersion": report.SingBoxVersion,
		"binaryPath":     report.BinaryPath,
		"serviceName":    report.ServiceName,
		"stages":         stages,
	}
}
