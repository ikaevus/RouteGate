package observability

import (
	"encoding/json"
	"strings"
	"time"
)

const DiagnosticResultSchemaVersion = 1

type agentDiagnosticEnvelope struct {
	SchemaVersion int       `json:"schemaVersion"`
	ProfileKey    string    `json:"profileKey"`
	CollectedAt   time.Time `json:"collectedAt"`
	Evidence      struct {
		Available bool                  `json:"available"`
		Host      AgentHostTelemetry    `json:"host"`
		VPNCore   AgentVPNCoreTelemetry `json:"vpnCore"`
	} `json:"evidence"`
}

type DiagnosticEvaluation struct {
	State             HealthState
	ReasonCode        string
	Summary           string
	RecommendedAction string
}

func EvaluateDiagnosticResult(profileKey string, raw json.RawMessage, resource ResourceRef, now time.Time) DiagnosticEvaluation {
	profileKey = strings.TrimSpace(profileKey)
	var envelope agentDiagnosticEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return invalidDiagnosticEvaluation("diagnostic_result_invalid", "Diagnostic result could not be interpreted.")
	}
	if envelope.SchemaVersion != DiagnosticResultSchemaVersion || strings.TrimSpace(envelope.ProfileKey) != profileKey {
		return invalidDiagnosticEvaluation("diagnostic_contract_mismatch", "Diagnostic result does not match the requested profile contract.")
	}
	if !envelope.Evidence.Available {
		return invalidDiagnosticEvaluation("diagnostic_evidence_unavailable", "Diagnostic evidence is unavailable.")
	}

	observedAt := now.UTC()
	expiresAt := observedAt.Add(time.Hour)
	switch profileKey {
	case DiagnosticProfileHostOverview:
		checks := []HealthCheck{
			evaluateMemoryHealth(resource, envelope.Evidence.Host, observedAt, expiresAt),
			evaluateDiskHealth(resource, envelope.Evidence.Host, observedAt, expiresAt),
		}
		aggregate := AggregateRequiredHealth(checks, observedAt)
		selected := diagnosticRepresentativeCheck(checks, aggregate.State, observedAt)
		if aggregate.State == HealthHealthy {
			return DiagnosticEvaluation{
				State:      HealthHealthy,
				ReasonCode: "host_overview_healthy",
				Summary:    "Host memory and root filesystem capacity are healthy.",
			}
		}
		return DiagnosticEvaluation{
			State:             aggregate.State,
			ReasonCode:        selected.ReasonCode,
			Summary:           selected.Summary,
			RecommendedAction: selected.RecommendedAction,
		}
	case DiagnosticProfileVPNCoreStatus:
		check := evaluateVPNCoreHealth(resource, envelope.Evidence.VPNCore, observedAt, expiresAt)
		return DiagnosticEvaluation{
			State:             check.State,
			ReasonCode:        check.ReasonCode,
			Summary:           check.Summary,
			RecommendedAction: check.RecommendedAction,
		}
	default:
		return invalidDiagnosticEvaluation("diagnostic_profile_unsupported", "Diagnostic profile is not supported by this Manager version.")
	}
}

func diagnosticRepresentativeCheck(checks []HealthCheck, state HealthState, now time.Time) HealthCheck {
	for _, check := range checks {
		if check.EffectiveState(now) == state {
			return check
		}
	}
	return HealthCheck{
		State:             state,
		ReasonCode:        "diagnostic_state_unknown",
		Summary:           "Diagnostic state requires attention.",
		RecommendedAction: "retry_diagnostic",
	}
}

func invalidDiagnosticEvaluation(reasonCode, summary string) DiagnosticEvaluation {
	return DiagnosticEvaluation{
		State:             HealthUnknown,
		ReasonCode:        reasonCode,
		Summary:           summary,
		RecommendedAction: "retry_diagnostic",
	}
}
