package observability

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const diagnosticPayloadSchemaVersion = 1

type diagnosticEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	ProfileKey    string          `json:"profileKey"`
	CollectedAt   time.Time       `json:"collectedAt"`
	Evidence      json.RawMessage `json:"evidence"`
}

type hostDiagnosticEvidence struct {
	Available bool               `json:"available"`
	Hostname  string             `json:"hostname"`
	OS        string             `json:"os"`
	Arch      string             `json:"arch"`
	Host      AgentHostTelemetry `json:"host"`
}

type vpnCoreDiagnosticEvidence struct {
	Available bool                  `json:"available"`
	VPNCore   AgentVPNCoreTelemetry `json:"vpnCore"`
}

type managerCertificateDiagnosticEvidence struct {
	Available bool      `json:"available"`
	Hostname  string    `json:"hostname"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	Verified  bool      `json:"verified"`
}

// EvaluateDiagnosticPayload is the trust boundary between Agent observations
// and RouteGate operational meaning. Agent may report facts, but it cannot set
// health state, reason codes, summaries, or recommended actions.
func EvaluateDiagnosticPayload(profileKey string, payload map[string]any, resource ResourceRef) (DiagnosticResult, map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return DiagnosticResult{}, nil, fmt.Errorf("encode diagnostic payload: %w", err)
	}
	var envelope diagnosticEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return DiagnosticResult{}, nil, fmt.Errorf("decode diagnostic payload: %w", err)
	}
	profileKey = strings.TrimSpace(profileKey)
	if envelope.SchemaVersion != diagnosticPayloadSchemaVersion {
		return DiagnosticResult{}, nil, fmt.Errorf("unsupported diagnostic payload schema version %d", envelope.SchemaVersion)
	}
	if strings.TrimSpace(envelope.ProfileKey) != profileKey {
		return DiagnosticResult{}, nil, fmt.Errorf("diagnostic profile mismatch")
	}
	if envelope.CollectedAt.IsZero() {
		return DiagnosticResult{}, nil, fmt.Errorf("diagnostic collectedAt is required")
	}

	switch profileKey {
	case DiagnosticProfileHostOverview:
		return evaluateHostDiagnostic(envelope, resource)
	case DiagnosticProfileVPNCoreStatus:
		return evaluateVPNCoreDiagnostic(envelope, resource)
	case DiagnosticProfileManagerCertificate:
		return evaluateManagerCertificateDiagnostic(envelope)
	default:
		return DiagnosticResult{}, nil, fmt.Errorf("unsupported diagnostic profile %q", profileKey)
	}
}

func evaluateManagerCertificateDiagnostic(envelope diagnosticEnvelope) (DiagnosticResult, map[string]any, error) {
	var evidence managerCertificateDiagnosticEvidence
	if len(envelope.Evidence) == 0 || string(envelope.Evidence) == "null" {
		return DiagnosticResult{}, nil, fmt.Errorf("manager certificate diagnostic evidence is required")
	}
	if err := json.Unmarshal(envelope.Evidence, &evidence); err != nil {
		return DiagnosticResult{}, nil, fmt.Errorf("decode manager certificate diagnostic evidence: %w", err)
	}
	if !evidence.Available {
		result := DiagnosticResult{
			CheckKey:          DiagnosticProfileManagerCertificate,
			State:             HealthUnknown,
			ReasonCode:        "manager_certificate_unavailable",
			Summary:           "Manager TLS certificate could not be inspected from this node.",
			RecommendedAction: "check_manager_tls",
			Evidence:          json.RawMessage(`{"available":false}`),
		}
		return result, diagnosticResultPayload(envelope, result, map[string]any{"available": false}), nil
	}
	if strings.TrimSpace(evidence.Hostname) == "" || evidence.NotBefore.IsZero() || evidence.NotAfter.IsZero() || !evidence.NotAfter.After(evidence.NotBefore) {
		return DiagnosticResult{}, nil, fmt.Errorf("manager certificate diagnostic evidence is incomplete")
	}

	result := DiagnosticResult{CheckKey: DiagnosticProfileManagerCertificate}
	switch {
	case !envelope.CollectedAt.Before(evidence.NotAfter):
		result.State = HealthUnhealthy
		result.ReasonCode = "manager_certificate_expired"
		result.Summary = "Manager TLS certificate has expired."
		result.RecommendedAction = "renew_manager_certificate"
	case envelope.CollectedAt.Before(evidence.NotBefore):
		result.State = HealthUnhealthy
		result.ReasonCode = "manager_certificate_not_yet_valid"
		result.Summary = "Manager TLS certificate is not valid yet."
		result.RecommendedAction = "check_manager_time_and_certificate"
	case !evidence.Verified:
		result.State = HealthUnhealthy
		result.ReasonCode = "manager_certificate_untrusted"
		result.Summary = "Manager TLS certificate chain or hostname could not be verified."
		result.RecommendedAction = "repair_manager_certificate"
	case evidence.NotAfter.Sub(envelope.CollectedAt) <= 30*24*time.Hour:
		result.State = HealthDegraded
		result.ReasonCode = "manager_certificate_expiring"
		result.Summary = "Manager TLS certificate expires within 30 days."
		result.RecommendedAction = "renew_manager_certificate"
	default:
		result.State = HealthHealthy
		result.ReasonCode = "manager_certificate_valid"
		result.Summary = "Manager TLS certificate is valid for more than 30 days."
	}

	safeEvidence := map[string]any{
		"available": true,
		"hostname":  boundedDiagnosticString(evidence.Hostname, 255),
		"notBefore": evidence.NotBefore.UTC(),
		"notAfter":  evidence.NotAfter.UTC(),
		"verified":  evidence.Verified,
	}
	encodedEvidence, _ := json.Marshal(safeEvidence)
	result.Evidence = encodedEvidence
	return result, diagnosticResultPayload(envelope, result, safeEvidence), nil
}

func evaluateHostDiagnostic(envelope diagnosticEnvelope, resource ResourceRef) (DiagnosticResult, map[string]any, error) {
	var evidence hostDiagnosticEvidence
	if len(envelope.Evidence) == 0 || string(envelope.Evidence) == "null" {
		return DiagnosticResult{}, nil, fmt.Errorf("host diagnostic evidence is required")
	}
	if err := json.Unmarshal(envelope.Evidence, &evidence); err != nil {
		return DiagnosticResult{}, nil, fmt.Errorf("decode host diagnostic evidence: %w", err)
	}
	if !evidence.Available {
		result := DiagnosticResult{
			CheckKey:          DiagnosticProfileHostOverview,
			State:             HealthUnknown,
			ReasonCode:        "diagnostic_evidence_unavailable",
			Summary:           "Host diagnostic evidence is unavailable.",
			RecommendedAction: "check_agent_telemetry",
			Evidence:          json.RawMessage(`{"available":false}`),
		}
		return result, diagnosticResultPayload(envelope, result, map[string]any{"available": false}), nil
	}
	if err := validateDiagnosticHost(evidence.Host); err != nil {
		return DiagnosticResult{}, nil, err
	}

	expiresAt := envelope.CollectedAt.Add(AgentTelemetryHealthTTL)
	checks := []HealthCheck{
		evaluateMemoryHealth(resource, evidence.Host, envelope.CollectedAt, expiresAt),
		evaluateDiskHealth(resource, evidence.Host, envelope.CollectedAt, expiresAt),
	}
	aggregate := AggregateRequiredHealth(checks, envelope.CollectedAt)
	result := DiagnosticResult{CheckKey: DiagnosticProfileHostOverview, State: aggregate.State}
	selected := selectDiagnosticCheck(checks, aggregate.State)
	if selected != nil {
		result.ReasonCode = selected.ReasonCode
		result.Summary = selected.Summary
		result.RecommendedAction = selected.RecommendedAction
	} else {
		result.ReasonCode = "host_overview_healthy"
		result.Summary = "Host memory and disk capacity are healthy."
	}

	safeEvidence := map[string]any{
		"available": true,
		"hostname":  boundedDiagnosticString(evidence.Hostname, 255),
		"os":        boundedDiagnosticString(evidence.OS, 64),
		"arch":      boundedDiagnosticString(evidence.Arch, 32),
		"host":      evidence.Host,
		"checks":    diagnosticChecksPayload(checks),
	}
	encodedEvidence, _ := json.Marshal(safeEvidence)
	result.Evidence = encodedEvidence
	return result, diagnosticResultPayload(envelope, result, safeEvidence), nil
}

func evaluateVPNCoreDiagnostic(envelope diagnosticEnvelope, resource ResourceRef) (DiagnosticResult, map[string]any, error) {
	var evidence vpnCoreDiagnosticEvidence
	if len(envelope.Evidence) == 0 || string(envelope.Evidence) == "null" {
		return DiagnosticResult{}, nil, fmt.Errorf("vpn core diagnostic evidence is required")
	}
	if err := json.Unmarshal(envelope.Evidence, &evidence); err != nil {
		return DiagnosticResult{}, nil, fmt.Errorf("decode vpn core diagnostic evidence: %w", err)
	}
	if !evidence.Available {
		result := DiagnosticResult{
			CheckKey:          DiagnosticProfileVPNCoreStatus,
			State:             HealthUnknown,
			ReasonCode:        "diagnostic_evidence_unavailable",
			Summary:           "VPN Core diagnostic evidence is unavailable.",
			RecommendedAction: "check_agent_telemetry",
			Evidence:          json.RawMessage(`{"available":false}`),
		}
		return result, diagnosticResultPayload(envelope, result, map[string]any{"available": false}), nil
	}
	core := evidence.VPNCore
	core.Type = boundedDiagnosticString(core.Type, 64)
	core.Version = boundedDiagnosticString(core.Version, 256)
	core.ServiceState = boundedDiagnosticString(core.ServiceState, 64)
	if strings.TrimSpace(core.Type) == "" || strings.TrimSpace(core.ServiceState) == "" {
		return DiagnosticResult{}, nil, fmt.Errorf("vpn core diagnostic evidence is incomplete")
	}

	expiresAt := envelope.CollectedAt.Add(AgentTelemetryHealthTTL)
	check := evaluateVPNCoreHealth(resource, core, envelope.CollectedAt, expiresAt)
	result := DiagnosticResult{
		CheckKey:          DiagnosticProfileVPNCoreStatus,
		State:             check.State,
		ReasonCode:        check.ReasonCode,
		Summary:           check.Summary,
		RecommendedAction: check.RecommendedAction,
	}
	safeEvidence := map[string]any{"available": true, "vpnCore": core}
	encodedEvidence, _ := json.Marshal(safeEvidence)
	result.Evidence = encodedEvidence
	return result, diagnosticResultPayload(envelope, result, safeEvidence), nil
}

func validateDiagnosticHost(host AgentHostTelemetry) error {
	if err := validateNonNegativeFloat("host.load1", host.Load1); err != nil {
		return err
	}
	if err := validateNonNegativeFloat("host.load5", host.Load5); err != nil {
		return err
	}
	if err := validateNonNegativeFloat("host.load15", host.Load15); err != nil {
		return err
	}
	if host.LogicalCPUs != nil && *host.LogicalCPUs <= 0 {
		return fmt.Errorf("diagnostic host.logicalCpus must be positive")
	}
	if host.MemoryTotalBytes != nil && *host.MemoryTotalBytes == 0 {
		return fmt.Errorf("diagnostic host.memoryTotalBytes must be positive")
	}
	if host.MemoryTotalBytes != nil && host.MemoryAvailableBytes != nil && *host.MemoryAvailableBytes > *host.MemoryTotalBytes {
		return fmt.Errorf("diagnostic available memory exceeds total memory")
	}
	if host.RootFSTotalBytes != nil && *host.RootFSTotalBytes == 0 {
		return fmt.Errorf("diagnostic host.rootFsTotalBytes must be positive")
	}
	if host.RootFSTotalBytes != nil && host.RootFSFreeBytes != nil && *host.RootFSFreeBytes > *host.RootFSTotalBytes {
		return fmt.Errorf("diagnostic free filesystem space exceeds total size")
	}
	return nil
}

func selectDiagnosticCheck(checks []HealthCheck, state HealthState) *HealthCheck {
	for i := range checks {
		if checks[i].State == state {
			return &checks[i]
		}
	}
	return nil
}

func diagnosticChecksPayload(checks []HealthCheck) []map[string]any {
	items := make([]map[string]any, 0, len(checks))
	for _, check := range checks {
		items = append(items, map[string]any{
			"checkKey":          check.Key,
			"state":             check.State,
			"reasonCode":        check.ReasonCode,
			"summary":           check.Summary,
			"recommendedAction": check.RecommendedAction,
			"evidence":          check.Evidence,
		})
	}
	return items
}

func diagnosticResultPayload(envelope diagnosticEnvelope, result DiagnosticResult, evidence map[string]any) map[string]any {
	return map[string]any{
		"schemaVersion":      diagnosticPayloadSchemaVersion,
		"profileKey":         strings.TrimSpace(envelope.ProfileKey),
		"collectedAt":        envelope.CollectedAt.UTC(),
		"state":              result.State,
		"reasonCode":         result.ReasonCode,
		"summary":            result.Summary,
		"recommendedAction":  result.RecommendedAction,
		"evidence":           evidence,
	}
}

func boundedDiagnosticString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
