package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidatorRunsSingBoxCheck(t *testing.T) {
	var gotName string
	var gotArgs []string
	validator := NewValidator("sing-box")
	validator.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName = name
		gotArgs = args
		return []byte("ok"), nil
	}

	result, err := validator.Check(context.Background(), "/tmp/config.json")
	if err != nil {
		t.Fatalf("check config: %v", err)
	}
	if gotName != "sing-box" || strings.Join(gotArgs, " ") != "check -c /tmp/config.json" {
		t.Fatalf("command = %s %v", gotName, gotArgs)
	}
	if result.Command != "sing-box check -c /tmp/config.json" || result.Output != "ok" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestValidatorReturnsCommandOutputOnFailure(t *testing.T) {
	validator := NewValidator("sing-box")
	validator.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("invalid config"), errors.New("exit status 1")
	}

	result, err := validator.Check(context.Background(), "/tmp/config.json")
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if result.Output != "invalid config" || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestValidatorRejectsEmptyConfigPath(t *testing.T) {
	_, err := NewValidator("sing-box").Check(context.Background(), "")
	if err == nil {
		t.Fatal("expected empty config path to fail")
	}
}
