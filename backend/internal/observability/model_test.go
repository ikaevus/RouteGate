package observability

import (
	"testing"
	"time"
)

func TestHealthStateContractContainsOnlyFourOperationalStates(t *testing.T) {
	valid := []HealthState{
		HealthHealthy,
		HealthDegraded,
		HealthUnhealthy,
		HealthUnknown,
	}
	for _, state := range valid {
		if !state.Valid() {
			t.Fatalf("health state %q must be valid", state)
		}
	}

	if HealthState("maintenance").Valid() {
		t.Fatal("maintenance must not become a fifth health state")
	}
}

func TestHealthCheckEffectiveStateBecomesUnknownWhenEvidenceExpires(t *testing.T) {
	observedAt := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	expiresAt := observedAt.Add(30 * time.Second)
	check := HealthCheck{
		Key:        "host.disk.capacity",
		State:      HealthHealthy,
		ObservedAt: observedAt,
		ExpiresAt:  &expiresAt,
	}

	if got := check.EffectiveState(expiresAt.Add(-time.Nanosecond)); got != HealthHealthy {
		t.Fatalf("fresh state = %q, want %q", got, HealthHealthy)
	}
	if got := check.EffectiveState(expiresAt); got != HealthUnknown {
		t.Fatalf("expired state = %q, want %q", got, HealthUnknown)
	}
}

func TestAlertStateTransitionsDoNotReopenResolvedEpisode(t *testing.T) {
	tests := []struct {
		from AlertState
		to   AlertState
		want bool
	}{
		{AlertPending, AlertFiring, true},
		{AlertPending, AlertResolved, true},
		{AlertFiring, AlertResolved, true},
		{AlertFiring, AlertPending, false},
		{AlertResolved, AlertFiring, false},
		{AlertResolved, AlertPending, false},
		{AlertFiring, AlertFiring, false},
	}

	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
			t.Fatalf("CanTransitionTo(%q -> %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestAcknowledgementDoesNotResolveFiringAlert(t *testing.T) {
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	alert := Alert{
		State:          AlertFiring,
		AcknowledgedAt: &now,
	}

	if !alert.Active() {
		t.Fatal("acknowledged firing alert must remain active")
	}
	if !alert.Acknowledged() {
		t.Fatal("alert must report acknowledgement independently from condition state")
	}
}

func TestDiagnosticStatusContractDoesNotPermitArbitraryShellState(t *testing.T) {
	valid := []DiagnosticStatus{
		DiagnosticQueued,
		DiagnosticRunning,
		DiagnosticSucceeded,
		DiagnosticFailed,
	}
	for _, status := range valid {
		if !status.Valid() {
			t.Fatalf("diagnostic status %q must be valid", status)
		}
	}

	if DiagnosticStatus("shell").Valid() {
		t.Fatal("arbitrary shell execution must not be represented as a diagnostic status")
	}
}
