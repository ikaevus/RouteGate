package tasks

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteServiceTaskRunsAllowListedOperation(t *testing.T) {
	controller := NewServiceController("sing-box")
	controller.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "systemctl" || strings.Join(args, " ") != "start sing-box" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return []byte("started"), nil
	}

	report, err := ExecuteServiceTask(context.Background(), controller, ConfigTask{
		ID:        "job-1",
		Kind:      TaskKindVPNCoreService,
		Operation: string(ServiceOperationStart),
	})
	if err != nil {
		t.Fatalf("execute service task: %v", err)
	}
	if report.Kind != TaskKindVPNCoreService || report.Operation != "start" || report.Service != "sing-box" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Command != "systemctl start sing-box" || report.Output != "started" {
		t.Fatalf("unexpected command report: %+v", report)
	}
}

func TestExecuteServiceTaskRejectsConfigTask(t *testing.T) {
	controller := NewServiceController("sing-box")
	controller.run = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("command runner must not be called")
		return nil, nil
	}

	_, err := ExecuteServiceTask(context.Background(), controller, ConfigTask{Kind: TaskKindConfigApply})
	if err == nil || !strings.Contains(err.Error(), "unsupported task kind") {
		t.Fatalf("expected task kind error, got %v", err)
	}
}

func TestEffectiveKindKeepsLegacyConfigTasksCompatible(t *testing.T) {
	if got := (ConfigTask{}).EffectiveKind(); got != TaskKindConfigApply {
		t.Fatalf("effective kind = %q", got)
	}
}

func TestVPNCoreInstallationTaskContract(t *testing.T) {
	task := ConfigTask{Kind: TaskKindVPNCoreInstall, Operation: InstallOperationSingBox}
	if task.EffectiveKind() != TaskKindVPNCoreInstall {
		t.Fatalf("kind = %q", task.EffectiveKind())
	}
	if task.Operation != InstallOperationSingBox {
		t.Fatalf("operation = %q", task.Operation)
	}
}
