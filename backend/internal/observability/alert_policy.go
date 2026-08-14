package observability

import (
	"strings"
	"time"
)

const (
	AlertEvaluationInterval = 30 * time.Second
	AlertCriticalFireDelay  = 30 * time.Second
	AlertWarningFireDelay   = 2 * time.Minute
	AlertRecoveryDelay      = 60 * time.Second
	AlertFlapWindow         = 5 * time.Minute
	AlertFlapFireDelay      = 3 * time.Minute
)

type AlertCondition struct {
	Triggered   bool
	Fingerprint string
	RuleKey     string
	Resource    ResourceRef
	Severity    Severity
	ReasonCode  string
	Summary     string
	FireAfter   time.Duration
}

func EvaluateAlertCondition(check HealthCheck, now time.Time) AlertCondition {
	state := check.EffectiveState(now)
	expired := check.ExpiresAt != nil && !now.Before(*check.ExpiresAt)

	// One stale Agent should produce one connectivity alert rather than separate
	// memory/disk/VPN alerts from evidence that merely became old at the same time.
	if check.Key != CheckAgentTelemetryFreshness && (expired || check.ReasonCode == "telemetry_stale") {
		return AlertCondition{}
	}
	// Partial telemetry can be missing while the heartbeat itself is healthy.
	// Unknown component evidence is diagnostic context, not an incident by itself.
	if check.Key != CheckAgentTelemetryFreshness && state == HealthUnknown {
		return AlertCondition{}
	}
	// During guided setup the absence of VPN Core is an expected lifecycle state.
	// Runtime alerting begins once a core exists and subsequently becomes unhealthy.
	if check.Key == CheckVPNCoreService && check.ReasonCode == "vpn_core_not_installed" {
		return AlertCondition{}
	}
	if state == HealthHealthy {
		return AlertCondition{}
	}

	condition := AlertCondition{
		Triggered:   true,
		Fingerprint: alertFingerprint(check.Key, check.Resource),
		RuleKey:     check.Key,
		Resource:    check.Resource,
		ReasonCode:  strings.TrimSpace(check.ReasonCode),
		Summary:     strings.TrimSpace(check.Summary),
	}
	if expired && check.Key == CheckAgentTelemetryFreshness {
		condition.ReasonCode = "telemetry_stale"
		condition.Summary = "Agent telemetry is stale."
	}

	switch state {
	case HealthUnhealthy:
		condition.Severity = SeverityCritical
		condition.FireAfter = AlertCriticalFireDelay
	case HealthDegraded:
		condition.Severity = SeverityWarning
		condition.FireAfter = AlertWarningFireDelay
	case HealthUnknown:
		if check.Key != CheckAgentTelemetryFreshness {
			return AlertCondition{}
		}
		condition.Severity = SeverityCritical
		condition.FireAfter = AlertCriticalFireDelay
	default:
		return AlertCondition{}
	}

	if condition.ReasonCode == "" {
		condition.ReasonCode = "health_condition_active"
	}
	if condition.Summary == "" {
		condition.Summary = "A RouteGate health condition requires attention."
	}
	return condition
}

func alertFingerprint(checkKey string, resource ResourceRef) string {
	return strings.Join([]string{strings.TrimSpace(checkKey), strings.TrimSpace(resource.Type), strings.TrimSpace(resource.ID)}, ":")
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 2
	case SeverityWarning:
		return 1
	default:
		return 0
	}
}

func effectiveFireDelay(base time.Duration, alertStartedAt time.Time, previousResolvedAt *time.Time) time.Duration {
	if previousResolvedAt == nil {
		return base
	}
	if alertStartedAt.Before(previousResolvedAt.Add(AlertFlapWindow)) && base < AlertFlapFireDelay {
		return AlertFlapFireDelay
	}
	return base
}
