package observability

import "time"

type HealthAggregate struct {
	State     HealthState `json:"state"`
	Required  int         `json:"required"`
	Healthy   int         `json:"healthy"`
	Degraded  int         `json:"degraded"`
	Unhealthy int         `json:"unhealthy"`
	Unknown   int         `json:"unknown"`
}

// AggregateRequiredHealth summarizes only required checks. Optional checks can
// enrich diagnostics without making the primary operational status noisy.
func AggregateRequiredHealth(checks []HealthCheck, now time.Time) HealthAggregate {
	result := HealthAggregate{State: HealthUnknown}
	for _, check := range checks {
		if !check.Required {
			continue
		}
		result.Required++
		switch check.EffectiveState(now) {
		case HealthHealthy:
			result.Healthy++
		case HealthDegraded:
			result.Degraded++
		case HealthUnhealthy:
			result.Unhealthy++
		default:
			result.Unknown++
		}
	}

	if result.Required == 0 {
		return result
	}
	switch {
	case result.Unhealthy > 0:
		result.State = HealthUnhealthy
	case result.Degraded > 0:
		result.State = HealthDegraded
	case result.Unknown > 0:
		result.State = HealthUnknown
	default:
		result.State = HealthHealthy
	}
	return result
}
