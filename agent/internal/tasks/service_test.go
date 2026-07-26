package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestServiceControllerExecutesAllowedOperations(t *testing.T) {
	tests := []struct {
		name      string
		operation ServiceOperation
		wantArgs  string
	}{
		{name: "start", operation: ServiceOperationStart, wantArgs: "start sing-box"},
		{name: "stop", operation: ServiceOperationStop, wantArgs: "stop sing-box"},
		{name: "restart", operation: ServiceOperationRestart, wantArgs: "restart sing-box"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotName string
			var gotArgs []string
			controller := NewServiceController("sing-box")
			controller.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
				gotName = name
				gotArgs = args
				return []byte(""), nil
			}

			result, err := controller.Execute(context.Background(), tt.operation)
			if err != nil {
				t.Fatalf("execute service operation: %v", err)
			}
			if gotName != "systemctl" || strings.Join(gotArgs, " ") != tt.wantArgs {
				t.Fatalf("command = %s %v", gotName, gotArgs)
			}
			if result.Operation != tt.operation {
				t.Fatalf("operation = %q", result.Operation)
			}
			if result.Command != "systemctl "+tt.wantArgs {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestServiceControllerConvenienceMethods(t *testing.T) {
	var commands []string
	controller := NewServiceController("sing-box")
	controller.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(args, " "))
		return nil, nil
	}

	if _, err := controller.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := controller.Restart(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}

	got := strings.Join(commands, ",")
	want := "start sing-box,stop sing-box,restart sing-box"
	if got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
}

func TestServiceControllerRejectsUnknownOperationWithoutExecutingCommand(t *testing.T) {
	called := false
	controller := NewServiceController("sing-box")
	controller.run = func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	}

	result, err := controller.Execute(context.Background(), ServiceOperation("reload; rm -rf /"))
	if err == nil {
		t.Fatal("expected unsupported operation error")
	}
	if called {
		t.Fatal("command runner must not be called for an unsupported operation")
	}
	if result.Operation != ServiceOperation("reload; rm -rf /") {
		t.Fatalf("operation = %q", result.Operation)
	}
}

func TestServiceControllerChecksHealth(t *testing.T) {
	var gotArgs []string
	controller := NewServiceController("sing-box")
	controller.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte(""), nil
	}

	result, err := controller.IsActive(context.Background())
	if err != nil {
		t.Fatalf("check service health: %v", err)
	}
	if strings.Join(gotArgs, " ") != "is-active --quiet sing-box" {
		t.Fatalf("args = %v", gotArgs)
	}
	if result.Command != "systemctl is-active --quiet sing-box" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Operation != "" {
		t.Fatalf("healthcheck operation = %q", result.Operation)
	}
}

func TestServiceControllerReturnsCommandOutputOnFailure(t *testing.T) {
	controller := NewServiceController("sing-box")
	controller.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("failed"), errors.New("exit status 1")
	}

	result, err := controller.Restart(context.Background())
	if err == nil {
		t.Fatal("expected restart failure")
	}
	if result.Operation != ServiceOperationRestart {
		t.Fatalf("operation = %q", result.Operation)
	}
	if result.Output != "failed" || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
