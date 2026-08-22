package heartbeat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
	"github.com/ikaevus/routegate/agent/internal/vpncoreinstall"
)

const installedServiceEnableTimeout = 10 * time.Second
const wireGuardPrerequisiteTimeout = 2 * time.Minute
const wireGuardSysctlPath = "/etc/sysctl.d/99-routegate-wireguard.conf"
const mtprotoServiceOverrideDir = "/etc/systemd/system/routegate-mtproto.service.d"
const mtprotoServiceOverridePath = mtprotoServiceOverrideDir + "/10-routegate-credentials.conf"

const mtprotoServiceCredentialOverride = `[Service]
ExecStart=
ExecStart=/usr/local/bin/mtg run ${CREDENTIALS_DIRECTORY}/mtg-config
LoadCredential=mtg-config:/etc/routegate-mtproto/config.toml
`

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
	if err := ensureWireGuardHostPrerequisites(ctx, &report); err != nil {
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), reportMap(report)); completeErr != nil {
			return fmt.Errorf("prepare WireGuard host prerequisites: %v; report failure: %w", err, completeErr)
		}
		return err
	}
	if err := ensureMTProtoCredentialAccess(ctx, &report); err != nil {
		if completeErr := r.client.CompleteTaskFailed(ctx, r.cfg.AgentToken, task.ID, err.Error(), reportMap(report)); completeErr != nil {
			return fmt.Errorf("prepare MTProto service credentials: %v; report failure: %w", err, completeErr)
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

// WireGuard's managed configuration uses iptables forwarding/NAT hooks and
// requires IPv4 forwarding on the host. Runtime installation is the right place
// to reconcile these host prerequisites because it is idempotent and runs before
// the transactional config apply.
func ensureWireGuardHostPrerequisites(ctx context.Context, report *vpncoreinstall.Report) error {
	if report == nil || strings.TrimSpace(report.Operation) != vpncoreinstall.OperationInstallWireGuard {
		return nil
	}

	prereqCtx, cancel := context.WithTimeout(ctx, wireGuardPrerequisiteTimeout)
	defer cancel()
	if err := exec.CommandContext(prereqCtx, "/usr/bin/apt-get", "install", "--yes", "--no-install-recommends", "iptables").Run(); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_host", Status: "failed", Code: "iptables_install_failed"})
		return fmt.Errorf("wireguard_iptables_install_failed")
	}
	if err := os.WriteFile(wireGuardSysctlPath, []byte("net.ipv4.ip_forward=1\n"), 0o644); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_host", Status: "failed", Code: "ip_forward_persistence_failed"})
		return fmt.Errorf("wireguard_ip_forward_persistence_failed")
	}
	if err := exec.CommandContext(prereqCtx, "/usr/sbin/sysctl", "-w", "net.ipv4.ip_forward=1").Run(); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_host", Status: "failed", Code: "ip_forward_enable_failed"})
		return fmt.Errorf("wireguard_ip_forward_enable_failed")
	}
	report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_host", Status: "succeeded"})
	return nil
}

// MTG runs with DynamicUser=true while RouteGate deliberately stores the
// FakeTLS secret in a root-only 0600 config. systemd LoadCredential bridges
// those requirements: systemd reads the protected source as PID 1 and exposes
// an isolated read-only copy only to the dynamic service user.
func ensureMTProtoCredentialAccess(ctx context.Context, report *vpncoreinstall.Report) error {
	if report == nil || strings.TrimSpace(report.Operation) != vpncoreinstall.OperationInstallMTG {
		return nil
	}
	if err := os.MkdirAll(mtprotoServiceOverrideDir, 0o755); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_service", Status: "failed", Code: "credential_override_directory_failed"})
		return fmt.Errorf("mtproto_credential_override_directory_failed")
	}
	if err := os.WriteFile(mtprotoServiceOverridePath, []byte(mtprotoServiceCredentialOverride), 0o644); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_service", Status: "failed", Code: "credential_override_write_failed"})
		return fmt.Errorf("mtproto_credential_override_write_failed")
	}
	reloadCtx, cancel := context.WithTimeout(ctx, installedServiceEnableTimeout)
	defer cancel()
	if err := exec.CommandContext(reloadCtx, "systemctl", "daemon-reload").Run(); err != nil {
		report.Status = "failed"
		report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_service", Status: "failed", Code: "credential_override_reload_failed"})
		return fmt.Errorf("mtproto_credential_override_reload_failed")
	}
	report.Stages = append(report.Stages, vpncoreinstall.StageResult{Stage: "prepare_service", Status: "succeeded"})
	return nil
}

// Runtime installers deliberately leave newly-created services inactive until a
// validated RouteGate configuration is present. They still need to be enabled
// for reboot persistence because the normal config-apply transaction verifies
// both runtime health and systemd enablement before it can commit the protocol
// as active.
func ensureInstalledServicePersistent(ctx context.Context, report *vpncoreinstall.Report) error {
	serviceName := installedRuntimeServiceName(report)
	if serviceName == "" {
		return nil
	}
	report.ServiceName = serviceName
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

func installedRuntimeServiceName(report *vpncoreinstall.Report) string {
	if report == nil {
		return ""
	}
	if serviceName := strings.TrimSpace(report.ServiceName); serviceName != "" {
		return serviceName
	}
	if strings.TrimSpace(report.Operation) == vpncoreinstall.OperationInstallWireGuard {
		return "wg-quick@routegate-wg0.service"
	}
	return ""
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
