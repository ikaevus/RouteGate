package observability

import (
	"encoding/json"
	"time"
)

const DiagnosticResultSchemaVersion = diagnosticPayloadSchemaVersion

type DiagnosticEvaluation struct {
	State             HealthState
	ReasonCode        string
	Summary           string
	RecommendedAction string
}

// EvaluateDiagnosticResult is a compatibility-facing evaluator for callers
// that hold the transport payload as raw JSON. The canonical trust-boundary
// implementation lives in EvaluateDiagnosticPayload so diagnostic meaning is
// defined in exactly one place.
func EvaluateDiagnosticResult(profileKey string, raw json.RawMessage, resource ResourceRef, _ time.Time) DiagnosticEvaluation {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return invalidDiagnosticEvaluation("diagnostic_result_invalid", "Diagnostic result could not be interpreted.")
	}
	result, _, err := EvaluateDiagnosticPayload(profileKey, payload, resource)
	if err != nil {
		return invalidDiagnosticEvaluation("diagnostic_result_invalid", "Diagnostic result could not be validated.")
	}
	return DiagnosticEvaluation{
		State:             result.State,
		ReasonCode:        result.ReasonCode,
		Summary:           result.Summary,
		RecommendedAction: result.RecommendedAction,
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
