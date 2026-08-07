package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const DefaultSingBoxServiceName = "sing-box"
const defaultServiceOperationTimeout = 15 * time.Second

type ServiceOperation string

const (
	ServiceOperationStart   ServiceOperation = "start"
	ServiceOperationStop    ServiceOperation = "stop"
	ServiceOperationRestart ServiceOperation = "restart"
	ServiceOperationEnable  ServiceOperation = "enable"
)

type ServiceResult struct {
	Operation ServiceOperation `json:"operation"`
	Command   string           `json:"command"`
	Output    string           `json:"output,omitempty"`
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

func (s ServiceController) Execute(ctx context.Context, operation ServiceOperation) (ServiceResult, error) {
	switch operation {
	case ServiceOperationStart, ServiceOperationStop, ServiceOperationRestart, ServiceOperationEnable:
		return s.systemctl(ctx, operation)
	default:
		return ServiceResult{Operation: operation}, fmt.Errorf("unsupported service operation %q", operation)
	}
}

func (s ServiceController) Start(ctx context.Context) (ServiceResult, error) {
	return s.Execute(ctx, ServiceOperationStart)
}

func (s ServiceController) Stop(ctx context.Context) (ServiceResult, error) {
	return s.Execute(ctx, ServiceOperationStop)
}

func (s ServiceController) Restart(ctx context.Context) (ServiceResult, error) {
	enableResult, err := s.Enable(ctx)
	if err != nil {
		return enableResult, fmt.Errorf("enable service before restart: %w", err)
	}
	restartResult, err := s.Execute(ctx, ServiceOperationRestart)
	restartResult.Command = enableResult.Command + " && " + restartResult.Command
	if enableResult.Output != "" {
		if restartResult.Output != "" {
			restartResult.Output = enableResult.Output + "\n" + restartResult.Output
		} else {
			restartResult.Output = enableResult.Output
		}
	}
	return restartResult, err
}

func (s ServiceController) Enable(ctx context.Context) (ServiceResult, error) {
	return s.Execute(ctx, ServiceOperationEnable)
}

func (s ServiceController) IsActive(ctx context.Context) (ServiceResult, error) {
	return s.systemctlCommand(ctx, "is-active", "--quiet")
}

func (s ServiceController) IsEnabled(ctx context.Context) (ServiceResult, error) {
	return s.systemctlCommand(ctx, "is-enabled", "--quiet")
}

func (s ServiceController) systemctl(ctx context.Context, operation ServiceOperation) (ServiceResult, error) {
	return s.runSystemctl(ctx, operation, string(operation))
}

func (s ServiceController) systemctlCommand(ctx context.Context, action string, extraArgs ...string) (ServiceResult, error) {
	return s.runSystemctl(ctx, "", action, extraArgs...)
}

func (s ServiceController) runSystemctl(ctx context.Context, operation ServiceOperation, action string, extraArgs ...string) (ServiceResult, error) {
	if strings.TrimSpace(s.service) == "" {
		return ServiceResult{Operation: operation}, fmt.Errorf("service name is required")
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
	result := ServiceResult{
		Operation: operation,
		Command:   "systemctl " + strings.Join(args, " "),
		Output:    strings.TrimSpace(string(output)),
	}
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
