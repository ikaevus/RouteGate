package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

var (
	canonicalPlatformUpdateTaskIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	canonicalRouteGateVersionPattern     = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	platformUpdateReceiptCodePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)
)

const (
	PlatformUpdateReceiptStatusPending        = "pending"
	PlatformUpdateReceiptStatusSucceeded      = "succeeded"
	PlatformUpdateReceiptStatusFailed         = "failed"
	PlatformUpdateReceiptStatusOutcomeUnknown = "outcome_unknown"
	maxPlatformUpdateReconciliationBytes      = 512
)

// PlatformUpdateReconciliationEvidence is the bounded receipt projection the
// Manager is allowed to consume from an Agent. It intentionally contains no
// filesystem path, URL, command, signer, trust-root, role, artifact selector,
// raw updater output, or other privileged input.
type PlatformUpdateReconciliationEvidence struct {
	TaskID        string `json:"taskId"`
	TargetVersion string `json:"targetVersion"`
	Status        string `json:"status"`
	Code          string `json:"code,omitempty"`
}

// DecodePlatformUpdateReconciliationEvidence converts the generic Agent result
// envelope back into the strict, bounded reconciliation contract. Unknown
// fields fail closed so result payloads cannot become a side channel for
// privileged selectors or unbounded updater output.
func DecodePlatformUpdateReconciliationEvidence(payload map[string]any) (PlatformUpdateReconciliationEvidence, error) {
	if payload == nil {
		return PlatformUpdateReconciliationEvidence{}, fmt.Errorf("platform update reconciliation evidence is required")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return PlatformUpdateReconciliationEvidence{}, fmt.Errorf("encode platform update reconciliation evidence: %w", err)
	}
	if len(data) == 0 || len(data) > maxPlatformUpdateReconciliationBytes {
		return PlatformUpdateReconciliationEvidence{}, fmt.Errorf("platform update reconciliation evidence exceeds bounded size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var evidence PlatformUpdateReconciliationEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return PlatformUpdateReconciliationEvidence{}, fmt.Errorf("decode platform update reconciliation evidence: %w", err)
	}
	if decoder.More() {
		return PlatformUpdateReconciliationEvidence{}, fmt.Errorf("platform update reconciliation evidence contains trailing data")
	}
	return evidence, nil
}

// ReconcilePlatformUpdateEvidence validates receipt identity and converts
// bounded Agent evidence into a Manager lifecycle transition. Missing evidence
// is represented by pending and never implies success or redispatch.
func ReconcilePlatformUpdateEvidence(expectedTaskID, expectedVersion string, evidence PlatformUpdateReconciliationEvidence) (string, error) {
	if !canonicalPlatformUpdateTaskIDPattern.MatchString(expectedTaskID) {
		return "", fmt.Errorf("expected platform update task id must be canonical UUIDv4")
	}
	if !canonicalRouteGateVersionPattern.MatchString(expectedVersion) {
		return "", fmt.Errorf("expected RouteGate version is not canonical")
	}
	if evidence.TaskID != expectedTaskID || evidence.TargetVersion != expectedVersion {
		return "", fmt.Errorf("platform update reconciliation identity mismatch")
	}

	switch evidence.Status {
	case PlatformUpdateReceiptStatusPending:
		if evidence.Code != "" {
			return "", fmt.Errorf("pending platform update evidence must not carry a code")
		}
		return AgentOperationJobStatusMutationDispatched, nil
	case PlatformUpdateReceiptStatusSucceeded:
		if evidence.Code != "" {
			return "", fmt.Errorf("successful platform update evidence must not carry a code")
		}
		return AgentOperationJobStatusSucceeded, nil
	case PlatformUpdateReceiptStatusFailed:
		if !platformUpdateReceiptCodePattern.MatchString(evidence.Code) {
			return "", fmt.Errorf("failed platform update evidence has invalid code")
		}
		return AgentOperationJobStatusFailed, nil
	case PlatformUpdateReceiptStatusOutcomeUnknown:
		if !platformUpdateReceiptCodePattern.MatchString(evidence.Code) {
			return "", fmt.Errorf("unknown platform update evidence has invalid code")
		}
		return AgentOperationJobStatusOutcomeUnknown, nil
	default:
		return "", fmt.Errorf("unsupported platform update reconciliation status")
	}
}
