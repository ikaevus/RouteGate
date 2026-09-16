package heartbeat

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/platform"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

type rollbackTestAdapter struct {
	stopCalls    int
	restartCalls int
}

func (a *rollbackTestAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{
		Core:          platform.VPNCoreHysteria,
		Protocol:      platform.VPNProtocolHysteria2,
		Transports:    []string{platform.VPNTransportQUIC},
		SecurityModes: []string{platform.VPNSecurityTLS},
	}
}

func (a *rollbackTestAdapter) Stage(tasks.ConfigTask) (tasks.StageResult, error) {
	return tasks.StageResult{}, nil
}

func (a *rollbackTestAdapter) Validate(context.Context, string) (tasks.ValidationResult, error) {
	return tasks.ValidationResult{}, nil
}

func (a *rollbackTestAdapter) Restart(context.Context) (tasks.ServiceResult, error) {
	a.restartCalls++
	return tasks.ServiceResult{}, nil
}

func (a *rollbackTestAdapter) Stop(context.Context) (tasks.ServiceResult, error) {
	a.stopCalls++
	return tasks.ServiceResult{}, nil
}

func (a *rollbackTestAdapter) IsActive(context.Context) (tasks.ServiceResult, error) {
	return tasks.ServiceResult{}, nil
}

func (a *rollbackTestAdapter) IsEnabled(context.Context) (tasks.ServiceResult, error) {
	return tasks.ServiceResult{}, nil
}

func (a *rollbackTestAdapter) ExecuteServiceTask(context.Context, tasks.ConfigTask) (tasks.ServiceTaskReport, error) {
	return tasks.ServiceTaskReport{}, nil
}

func (a *rollbackTestAdapter) CheckHealth(context.Context, string) (tasks.ListenerHealthResult, error) {
	return tasks.ListenerHealthResult{}, nil
}

func TestRollbackPreparedRuntimesStopsFirstTimeRuntime(t *testing.T) {
	dir := t.TempDir()
	activePath := filepath.Join(dir, "active", "config.json")
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(filepath.Dir(activePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, []byte("new config"), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := &rollbackTestAdapter{}
	runner := &Runner{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	status := runner.rollbackPreparedRuntimes(context.Background(), []preparedConfigRuntime{{
		adapter:    adapter,
		activePath: activePath,
		backupDir:  backupDir,
		apply: tasks.ApplyResult{
			ActivePath: activePath,
		},
	}})

	if status != "succeeded" {
		t.Fatalf("expected successful rollback, got %q", status)
	}
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Fatalf("expected first-time active config to be removed, stat error: %v", err)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("expected runtime to be stopped once, got %d", adapter.stopCalls)
	}
	if adapter.restartCalls != 0 {
		t.Fatalf("expected runtime not to be restarted, got %d restarts", adapter.restartCalls)
	}
}
