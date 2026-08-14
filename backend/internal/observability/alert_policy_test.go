package observability

import (
	"testing"
	"time"
)

func TestEvaluateAlertConditionSuppressesDuplicateStaleChecks(t *testing.T) {
	now := time.Now().UTC()
	expiresAt := now.Add(-time.Second)
	check := HealthCheck{
		Key:        CheckHostDiskCapacity,
		Resource:   ResourceRef{Type: "server", ID: "server-1"},
		State:      HealthHealthy,
		Required:   true,
		ReasonCode: "disk_capacity_ok",
		ExpiresAt:  &expiresAt,
	}
	if condition := EvaluateAlertCondition(check, now); condition.Triggered {
		t.Fatalf("expired non-freshness check must be suppressed: %+v", condition)
	}

	check.Key = CheckAgentTelemetryFreshness
	check.ReasonCode = "telemetry_recent"
	check.Summary = "Agent telemetry is current."
	condition := EvaluateAlertCondition(check, now)
	if !condition.Triggered || condition.Severity != SeverityCritical || condition.ReasonCode != "telemetry_stale" {
		t.Fatalf("expired freshness check = %+v, want critical telemetry_stale", condition)
	}
}

func TestEvaluateAlertConditionSkipsVPNCoreNotInstalledSetupState(t *testing.T) {
	condition := EvaluateAlertCondition(HealthCheck{
		Key:        CheckVPNCoreService,
		Resource:   ResourceRef{Type: "server", ID: "server-1"},
		State:      HealthUnhealthy,
		ReasonCode: "vpn_core_not_installed",
		Summary:    "VPN Core is not installed.",
	}, time.Now().UTC())
	if condition.Triggered {
		t.Fatalf("guided setup state must not create incident alert: %+v", condition)
	}
}

func TestEvaluateAlertConditionMapsSeverityAndDelay(t *testing.T) {
	now := time.Now().UTC()
	resource := ResourceRef{Type: "server", ID: "server-1"}

	warning := EvaluateAlertCondition(HealthCheck{
		Key:        CheckHostDiskCapacity,
		Resource:   resource,
		State:      HealthDegraded,
		ReasonCode: "disk_free_low",
		Summary:    "Root filesystem free space is low.",
	}, now)
	if !warning.Triggered || warning.Severity != SeverityWarning || warning.FireAfter != AlertWarningFireDelay {
		t.Fatalf("degraded condition = %+v", warning)
	}

	critical := EvaluateAlertCondition(HealthCheck{
		Key:        CheckHostDiskCapacity,
		Resource:   resource,
		State:      HealthUnhealthy,
		ReasonCode: "disk_free_critical",
		Summary:    "Root filesystem free space is critically low.",
	}, now)
	if !critical.Triggered || critical.Severity != SeverityCritical || critical.FireAfter != AlertCriticalFireDelay {
		t.Fatalf("unhealthy condition = %+v", critical)
	}
	if critical.Fingerprint != "host.disk.capacity:server:server-1" {
		t.Fatalf("fingerprint = %q", critical.Fingerprint)
	}
}

func TestEffectiveFireDelayDampensFastRecurrence(t *testing.T) {
	startedAt := time.Now().UTC()
	resolvedAt := startedAt.Add(-time.Minute)
	if got := effectiveFireDelay(AlertCriticalFireDelay, startedAt, &resolvedAt); got != AlertFlapFireDelay {
		t.Fatalf("flapping fire delay = %s, want %s", got, AlertFlapFireDelay)
	}

	resolvedAt = startedAt.Add(-AlertFlapWindow - time.Second)
	if got := effectiveFireDelay(AlertCriticalFireDelay, startedAt, &resolvedAt); got != AlertCriticalFireDelay {
		t.Fatalf("stable recurrence fire delay = %s, want %s", got, AlertCriticalFireDelay)
	}
}
