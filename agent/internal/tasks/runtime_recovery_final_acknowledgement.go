package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
)

// Mirrors Manager's historical storage receipt. Recorded is never release
// authority, and persistence never clears or resumes Agent mutation state.
type RuntimeRecoveryFinalizationReceipt struct {
	SchemaVersion        int    `json:"schemaVersion"`
	Recorded             bool   `json:"recorded"`
	OperationID          string `json:"operationId"`
	AuthorizationHash    string `json:"authorizationHash"`
	FinalConsumptionHash string `json:"finalConsumptionHash"`
	ProofHash            string `json:"proofHash"`
	RequestHash          string `json:"requestHash"`
}

// RecordReceipt may only be called with a still-live submission guard after
// transport verification. It independently binds the receipt to exact private
// pinned bytes and re-observes before and after atomic write/fsync. Publication
// uncertainty retains all files and poisons the owner; nothing is overwritten.
func (g *RuntimeRecoveryFinalizationGuard) RecordReceipt(ctx context.Context, r RuntimeRecoveryFinalizationReceipt) error {
	if g == nil || g.state == nil {
		return ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	if _, err := m.proofLocked(ctx); err != nil {
		return err
	}
	if m.submission == nil || r.SchemaVersion != 1 || !r.Recorded || r.OperationID != m.frozen.OperationID || r.AuthorizationHash != m.authorizationHash || r.FinalConsumptionHash != m.frozen.FinalConsumptionHash || r.ProofHash != markerDigest(m.submission) || r.RequestHash != r.ProofHash {
		return ErrRuntimeMutationBlocked
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > 4096 {
		return ErrRuntimeMutationBlocked
	}
	if m.acknowledgement != nil {
		if !bytes.Equal(data, m.acknowledgement) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}
	if writeRemovalFile(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile), data) != nil {
		m.poisoned = true
		return ErrRuntimeMutationBlocked
	}
	m.acknowledgement = data
	if _, err = m.proofLocked(ctx); err != nil {
		return err
	}
	return nil
}
