package observability

import (
	"testing"
	"time"
)

func TestAggregateRequiredHealthPrecedence(t *testing.T) {
	now := time.Now().UTC()
	checks := []HealthCheck{
		{Key: "healthy", Required: true, State: HealthHealthy},
		{Key: "unknown", Required: true, State: HealthUnknown},
		{Key: "degraded", Required: true, State: HealthDegraded},
		{Key: "unhealthy", Required: true, State: HealthUnhealthy},
		{Key: "optional-unhealthy", Required: false, State: HealthUnhealthy},
	}

	aggregate := AggregateRequiredHealth(checks, now)
	if aggregate.State != HealthUnhealthy {
		t.Fatalf("aggregate state = %q, want unhealthy", aggregate.State)
	}
	if aggregate.Required != 4 || aggregate.Healthy != 1 || aggregate.Unknown != 1 || aggregate.Degraded != 1 || aggregate.Unhealthy != 1 {
		t.Fatalf("unexpected aggregate counts: %+v", aggregate)
	}
}

func TestAggregateRequiredHealthExpiredEvidenceIsUnknown(t *testing.T) {
	now := time.Now().UTC()
	expiresAt := now.Add(-time.Second)
	checks := []HealthCheck{{
		Key:       "expired",
		Required:  true,
		State:     HealthHealthy,
		ExpiresAt: &expiresAt,
	}}

	aggregate := AggregateRequiredHealth(checks, now)
	if aggregate.State != HealthUnknown || aggregate.Unknown != 1 {
		t.Fatalf("expired aggregate = %+v, want unknown", aggregate)
	}
}

func TestAggregateRequiredHealthNoRequiredChecksIsUnknown(t *testing.T) {
	aggregate := AggregateRequiredHealth([]HealthCheck{{Required: false, State: HealthHealthy}}, time.Now().UTC())
	if aggregate.State != HealthUnknown || aggregate.Required != 0 {
		t.Fatalf("aggregate without required checks = %+v", aggregate)
	}
}
