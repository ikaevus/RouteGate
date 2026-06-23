package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestServiceControllerRestartsSingBox(t *testing.T) {
	var gotName string
	var gotArgs []string
	controller := NewServiceController("sing-box")
	controller.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = args
		return []byte(""), nil
	}

	result, err := controller.Restart(context.Background())
	if err != nil {
		t.Fatalf("restart service: %v", err)
	}
	if gotName != "systemctl" || strings.Join(gotArgs, " ") != "restart sing-box" {
		t.Fatalf("command = %s %v", gotName, gotArgs)
	}
	if result.Command != "systemctl restart sing-box" {
		t.Fatalf("unexpected result: %+v", result)
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
	if result.Output != "failed" || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
