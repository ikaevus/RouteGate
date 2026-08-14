package observability

import (
	"testing"
	"time"
)

func TestEvaluateAlertConditionSuppressesPartialUnknownEvidence(t *testing.T) {
	condition := EvaluateAlertCondition(HealthCheck{
		Key:        CheckHostMemoryCapacity,
		Resource:   ResourceRef{Type: "server", ID: "server-1"},
		State:      HealthUnknown,
		Required:   true,
		ReasonCode: "memory_capacity_unavailable",
		Summary:    "Memory capacity could not be evaluated.",
	}, time.Now().UTC())
	if condition.Triggered {
		t.Fatalf("partial unknown telemetry must not create an alert: %+v", condition)
	}
}
