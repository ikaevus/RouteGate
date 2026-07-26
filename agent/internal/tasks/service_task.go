package tasks

import (
	"context"
	"fmt"
	"strings"
)

type ServiceTaskReport struct {
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	Service   string `json:"service"`
	Command   string `json:"command"`
	Output    string `json:"output,omitempty"`
}

func ExecuteServiceTask(ctx context.Context, controller ServiceController, task ConfigTask) (ServiceTaskReport, error) {
	if task.EffectiveKind() != TaskKindVPNCoreService {
		return ServiceTaskReport{}, fmt.Errorf("unsupported task kind %q", task.EffectiveKind())
	}
	operation := ServiceOperation(strings.TrimSpace(task.Operation))
	result, err := controller.Execute(ctx, operation)
	report := ServiceTaskReport{
		Kind:      TaskKindVPNCoreService,
		Operation: string(operation),
		Service:   controller.service,
		Command:   result.Command,
		Output:    result.Output,
	}
	if err != nil {
		return report, err
	}
	return report, nil
}
