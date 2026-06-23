package tasks

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const DefaultSingBoxPath = "sing-box"
const defaultValidationTimeout = 10 * time.Second

type ValidationResult struct {
	Command string `json:"command"`
	Output  string `json:"output,omitempty"`
}

type commandRunner func(context.Context, string, ...string) ([]byte, error)

type Validator struct {
	binary  string
	timeout time.Duration
	run     commandRunner
}

func NewValidator(binary string) Validator {
	if strings.TrimSpace(binary) == "" {
		binary = DefaultSingBoxPath
	}
	return Validator{binary: binary, timeout: defaultValidationTimeout, run: runCommand}
}

func (v Validator) Check(ctx context.Context, configPath string) (ValidationResult, error) {
	if strings.TrimSpace(configPath) == "" {
		return ValidationResult{}, fmt.Errorf("config path is required")
	}
	if v.timeout <= 0 {
		v.timeout = defaultValidationTimeout
	}
	if v.run == nil {
		v.run = runCommand
	}

	checkCtx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	args := []string{"check", "-c", configPath}
	output, err := v.run(checkCtx, v.binary, args...)
	result := ValidationResult{Command: v.binary + " " + strings.Join(args, " "), Output: strings.TrimSpace(string(output))}
	if err != nil {
		if checkCtx.Err() != nil {
			return result, fmt.Errorf("sing-box check timed out: %w", checkCtx.Err())
		}
		if result.Output == "" {
			return result, fmt.Errorf("sing-box check failed: %w", err)
		}
		return result, fmt.Errorf("sing-box check failed: %s", result.Output)
	}
	return result, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
