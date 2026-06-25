package traffic

import (
	"testing"
	"time"
)

func trafficLimitInt64Ptr(value int64) *int64 {
	return &value
}

func TestEvaluateTrafficLimitEnforcementIgnoresMissingLimit(t *testing.T) {
	evaluation := evaluateTrafficLimitEnforcement(TrafficLimit{HardLimitEnabled: true}, 10_000, time.Now())

	if evaluation.Status != TrafficLimitEnforcementNotEnforced {
		t.Fatalf("expected not_enforced, got %q", evaluation.Status)
	}
	if evaluation.Enforced || evaluation.ExceededAt != nil {
		t.Fatalf("expected missing limit to stay unenforced: %+v", evaluation)
	}
}

func TestEvaluateTrafficLimitEnforcementIgnoresDisabledHardLimit(t *testing.T) {
	evaluation := evaluateTrafficLimitEnforcement(TrafficLimit{MonthlyLimitBytes: trafficLimitInt64Ptr(100)}, 150, time.Now())

	if evaluation.Status != TrafficLimitEnforcementNotEnforced {
		t.Fatalf("expected not_enforced, got %q", evaluation.Status)
	}
	if evaluation.Enforced || evaluation.ExceededAt != nil {
		t.Fatalf("expected disabled hard limit to stay unenforced: %+v", evaluation)
	}
}

func TestEvaluateTrafficLimitEnforcementWithinLimit(t *testing.T) {
	evaluation := evaluateTrafficLimitEnforcement(TrafficLimit{MonthlyLimitBytes: trafficLimitInt64Ptr(100), HardLimitEnabled: true}, 99, time.Now())

	if evaluation.Status != TrafficLimitEnforcementWithinLimit {
		t.Fatalf("expected within_limit, got %q", evaluation.Status)
	}
	if evaluation.Enforced || evaluation.ExceededAt != nil {
		t.Fatalf("expected within-limit account to stay unenforced: %+v", evaluation)
	}
}

func TestEvaluateTrafficLimitEnforcementPersistsExceededAt(t *testing.T) {
	evaluatedAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	evaluation := evaluateTrafficLimitEnforcement(TrafficLimit{MonthlyLimitBytes: trafficLimitInt64Ptr(100), HardLimitEnabled: true}, 100, evaluatedAt)

	if evaluation.Status != TrafficLimitEnforcementOverLimit {
		t.Fatalf("expected over_limit, got %q", evaluation.Status)
	}
	if !evaluation.Enforced {
		t.Fatalf("expected hard over-limit account to be enforced")
	}
	if evaluation.ExceededAt == nil || !evaluation.ExceededAt.Equal(evaluatedAt) {
		t.Fatalf("unexpected exceededAt: %+v", evaluation.ExceededAt)
	}
}

func TestEvaluateTrafficLimitEnforcementIsIdempotentAfterExceeded(t *testing.T) {
	firstExceededAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	laterEvaluation := time.Date(2026, time.June, 25, 13, 0, 0, 0, time.UTC)
	evaluation := evaluateTrafficLimitEnforcement(TrafficLimit{
		MonthlyLimitBytes: trafficLimitInt64Ptr(100),
		HardLimitEnabled:  true,
		LimitExceededAt:   &firstExceededAt,
	}, 150, laterEvaluation)

	if evaluation.Status != TrafficLimitEnforcementOverLimit {
		t.Fatalf("expected over_limit, got %q", evaluation.Status)
	}
	if evaluation.ExceededAt == nil || !evaluation.ExceededAt.Equal(firstExceededAt) {
		t.Fatalf("expected original exceededAt to be preserved, got %+v", evaluation.ExceededAt)
	}
}

func TestBuildLimitStateIncludesRemainingBytesAndEnforcement(t *testing.T) {
	exceededAt := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
	state := buildLimitState(TrafficLimit{
		MonthlyLimitBytes: trafficLimitInt64Ptr(100),
		HardLimitEnabled:  true,
		LimitExceededAt:   &exceededAt,
		EnforcementStatus: TrafficLimitEnforcementOverLimit,
		ResetDay:          DefaultResetDay,
		UpdatedAt:         exceededAt,
	}, 150)

	if state.RemainingBytes == nil || *state.RemainingBytes != 0 {
		t.Fatalf("expected zero remaining bytes, got %+v", state.RemainingBytes)
	}
	if !state.LimitReached || !state.Enforced {
		t.Fatalf("expected reached enforced limit state: %+v", state)
	}
	if state.EnforcementStatus != TrafficLimitEnforcementOverLimit {
		t.Fatalf("unexpected enforcement status: %q", state.EnforcementStatus)
	}
}
