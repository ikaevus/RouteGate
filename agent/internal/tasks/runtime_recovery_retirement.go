package tasks

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"time"
)

const runtimeRecoveryRetirementFile = "recovery-retirement-intent.json"
const runtimeRecoveryRetirementPurpose = "runtime_mutation_fence_retirement_intent_v1"

type runtimeRecoveryRetirementSource struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Hash    string `json:"hash"`
}

// Private prepared intent only, never proof that files/fence were released.
type runtimeRecoveryRetirementIntent struct {
	SchemaVersion        int                               `json:"schemaVersion"`
	Purpose              string                            `json:"purpose"`
	OperationID          string                            `json:"operationId"`
	Binding              RuntimeRecoveryGrantBinding       `json:"binding"`
	AuthorizationHash    string                            `json:"authorizationHash"`
	FinalConsumptionHash string                            `json:"finalConsumptionHash"`
	FinalProofHash       string                            `json:"finalProofHash"`
	FinalReceiptHash     string                            `json:"finalReceiptHash"`
	ObservedAt           time.Time                         `json:"observedAt"`
	Observation          RuntimeRecoveryObservation        `json:"observation"`
	Files                []runtimeRecoveryRetirementSource `json:"files"`
}

func retirementSourceManifest(dir string) ([]runtimeRecoveryRetirementSource, error) {
	if !runtimeMarkerAbsent(filepath.Join(dir, "inflight.json")) {
		return nil, ErrRuntimeMutationBlocked
	}
	paths := []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalConsumptionFile, runtimeRecoveryFinalSubmissionFile, runtimeRecoveryFinalAcknowledgementFile}
	limits := []int64{16384, runtimeResultLimit, runtimeRecoveryConsumptionLimit, 8192, 4096, runtimeRecoveryConsumptionLimit, 8192, 4096}
	files := make([]runtimeRecoveryRetirementSource, 0, len(paths))
	for i, name := range paths {
		if i == 4 && runtimeMarkerAbsent(filepath.Join(dir, name)) {
			files = append(files, runtimeRecoveryRetirementSource{Name: name})
			continue
		}
		data, e := readRemovalFile(filepath.Join(dir, name), limits[i], true)
		if e != nil {
			return nil, ErrRuntimeMutationBlocked
		}
		files = append(files, runtimeRecoveryRetirementSource{name, true, markerDigest(data)})
	}
	return files, nil
}

func validateRetirementIntent(data []byte, dir string, b RuntimeRecoveryGrantBinding, a RuntimeRecoveryReleaseAuthorization, p RuntimeRecoveryFinalizationProof, pin, receipt []byte) (runtimeRecoveryRetirementIntent, error) {
	var r runtimeRecoveryRetirementIntent
	deny := func() (runtimeRecoveryRetirementIntent, error) {
		return runtimeRecoveryRetirementIntent{}, ErrRuntimeMutationBlocked
	}
	if len(data) > 8192 || len(receipt) == 0 || !exactHistoricalJSON(data, &r) {
		return deny()
	}
	var ack RuntimeRecoveryFinalizationReceipt
	if !exactHistoricalJSON(receipt, &ack) || ack.SchemaVersion != 1 || !ack.Recorded || ack.OperationID != p.OperationID || ack.AuthorizationHash != p.AuthorizationHash || ack.FinalConsumptionHash != p.FinalConsumptionHash || ack.ProofHash != markerDigest(pin) || ack.RequestHash != ack.ProofHash {
		return deny()
	}
	files, e := retirementSourceManifest(dir)
	if e != nil || r.SchemaVersion != 1 || r.Purpose != runtimeRecoveryRetirementPurpose || r.OperationID != p.OperationID || r.Binding != b || r.AuthorizationHash != p.AuthorizationHash || r.FinalConsumptionHash != p.FinalConsumptionHash || r.FinalProofHash != markerDigest(pin) || r.FinalReceiptHash != markerDigest(receipt) || r.ObservedAt.Before(p.ObservedAt) || r.ObservedAt.Before(a.IssuedAt) || !r.ObservedAt.Before(a.ExpiresAt) || r.Observation != a.Expected || !slices.Equal(r.Files, files) {
		return deny()
	}
	return r, nil
}

// PrepareRetirement persists a barrier before future multi-file retirement.
// Only the still-live final guard with current authority, exact pin/receipt and
// fresh locked observation can call it. The returned digest is prepared history
// only; no deletion, release, resume or Manager reservation change occurs.
func (g *RuntimeRecoveryFinalizationGuard) PrepareRetirement(ctx context.Context) (string, error) {
	if g == nil || g.state == nil {
		return "", ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return "", ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	p, e := m.proofLocked(ctx)
	if e != nil {
		return "", ErrRuntimeMutationBlocked
	}
	if m.record == nil || m.submission == nil || m.acknowledgement == nil {
		return "", ErrRuntimeMutationBlocked
	}
	if m.retirement != nil {
		if _, e = validateRetirementIntent(m.retirement, m.dir, m.binding, m.authorization, m.frozen, m.submission, m.acknowledgement); e != nil {
			m.poisoned = true
			return "", ErrRuntimeMutationBlocked
		}
		if _, e = m.proofLocked(ctx); e != nil {
			return "", ErrRuntimeMutationBlocked
		}
		return markerDigest(m.retirement), nil
	}
	files, e := retirementSourceManifest(m.dir)
	if e != nil {
		m.poisoned = true
		return "", ErrRuntimeMutationBlocked
	}
	r := runtimeRecoveryRetirementIntent{1, runtimeRecoveryRetirementPurpose, p.OperationID, m.binding, p.AuthorizationHash, p.FinalConsumptionHash, markerDigest(m.submission), markerDigest(m.acknowledgement), p.ObservedAt, p.Observation, files}
	data, e := json.Marshal(r)
	if e != nil {
		return "", ErrRuntimeMutationBlocked
	}
	if _, e = validateRetirementIntent(data, m.dir, m.binding, m.authorization, m.frozen, m.submission, m.acknowledgement); e != nil {
		m.poisoned = true
		return "", ErrRuntimeMutationBlocked
	}
	// Repeat immediately before publication, after the manifest reads.
	if _, e = m.proofLocked(ctx); e != nil {
		return "", ErrRuntimeMutationBlocked
	}
	if writeRemovalFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile), data) != nil {
		m.poisoned = true
		return "", ErrRuntimeMutationBlocked
	}
	m.retirement = data
	if _, e = validateRetirementIntent(data, m.dir, m.binding, m.authorization, m.frozen, m.submission, m.acknowledgement); e != nil {
		m.poisoned = true
		return "", ErrRuntimeMutationBlocked
	}
	if _, e = m.proofLocked(ctx); e != nil {
		return "", ErrRuntimeMutationBlocked
	}
	return markerDigest(data), nil
}
