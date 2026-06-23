package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const DefaultSingBoxServiceName = "sing-box"
const defaultServiceOperationTimeout = 15 * time.Second

type ServiceResult struct {
	Command string `json:"command"`
	Output  string `json:"output,omitempty"`
}

type ServiceController struct {
	service string
	timeout time.Duration
	run     commandRunner
}

func NewServiceController(service string) ServiceController {
	if strings.TrimSpace(service) == "" {
		service = DefaultSingBoxServiceName
	}
	return ServiceController{service: service, timeout: defaultServiceOperationTimeout, run: runCommand}
}

func (s ServiceController) Restart(ctx context.Context) (ServiceResult, error) {
	return s.systemctl(ctx, "restart")
}

func (s ServiceController) IsActive(ctx context.Context) (ServiceResult, error) {
	return s.systemctl(ctx, "is-active", "--quiet")
}

func (s ServiceController) systemctl(ctx context.Context, action string, extraArgs ...string) (ServiceResult, error) {
	if strings.TrimSpace(s.service) == "" {
		return ServiceResult{}, fmt.Errorf("service name is required")
	}
	if s.timeout <= 0 {
		s.timeout = defaultServiceOperationTimeout
	}
	if s.run == nil {
		s.run = runCommand
	}

	checkCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	args := append([]string{action}, extraArgs...)
	args = append(args, s.service)
	output, err := s.run(checkCtx, "systemctl", args...)
	result := ServiceResult{Command: "systemctl " + strings.Join(args, " "), Output: strings.TrimSpace(string(output))}
	if err != nil {
		if checkCtx.Err() != nil {
			return result, fmt.Errorf("systemctl %s timed out: %w", action, checkCtx.Err())
		}
		if result.Output == "" {
			return result, fmt.Errorf("systemctl %s failed: %w", action, err)
		}
		return result, fmt.Errorf("systemctl %s failed: %s", action, result.Output)
	}
	return result, nil
}
