package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

const runtimeRecoveryRetentionSubmissionFile = "recovery-retention-submission.json"
const runtimeRecoveryRetentionAcknowledgementFile = "recovery-retention-acknowledgement.json"

// Callers have independently validated the complete prepared/retained chain.
func retentionProofForPrepared(p RuntimeRecoveryPreparedReleaseProof, ack []byte, hash string, retainedAt, observedAt time.Time) RuntimeRecoveryRetentionProof {
	wire, _ := json.Marshal(p)
	return RuntimeRecoveryRetentionProof{SchemaVersion: 1, Purpose: RuntimeRecoveryRetentionPurpose,
		OperationID: p.OperationID, AgentCredentialGeneration: p.AgentCredentialGeneration,
		AuthorizationHash: p.AuthorizationHash, PreparedProofHash: markerDigest(wire),
		PreparedReceiptHash: markerDigest(ack), RetentionHash: hash, RetainedAt: retainedAt,
		ObservedAt: observedAt, Observation: p.Observation}
}

func retentionReceiptMatches(r RuntimeRecoveryRetentionReceipt, p RuntimeRecoveryRetentionProof, wire []byte) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID && r.RetentionHash == p.RetentionHash && r.ProofHash == markerDigest(wire) && r.RequestHash == r.ProofHash
}

func (m *RuntimeRecoveryFinalization) retentionProofLocked(ctx context.Context) (RuntimeRecoveryRetentionProof, error) {
	var zero RuntimeRecoveryRetentionProof
	if m.releaseHistoryRetention == nil || m.preparedAcknowledgement == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoveryRetentionProof, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	p, e := m.preparedReleaseProofLocked(ctx)
	if e != nil {
		return deny()
	}
	var retained runtimeRecoveryReleaseRetention
	if !exactHistoricalJSON(m.releaseHistoryRetention, &retained) {
		return deny()
	}
	observed := m.lastChecked
	if m.retentionSubmission != nil {
		observed = m.retentionFrozen.ObservedAt
	}
	proof := retentionProofForPrepared(p, m.preparedAcknowledgement, markerDigest(m.releaseHistoryRetention), retained.RetainedAt, observed)
	if observed.IsZero() || observed.Before(retained.RetainedAt) || observed.After(m.lastChecked) || !observed.Before(m.authorization.ExpiresAt) {
		return deny()
	}
	if m.retentionSubmission != nil {
		wire, e := json.Marshal(proof)
		if e != nil || proof != m.retentionFrozen || !bytes.Equal(wire, m.retentionSubmission) {
			return deny()
		}
	}
	if !m.terminalAuthorityCurrent(ctx) {
		return deny()
	}
	return proof, nil
}

// Same callback-scoped guard and original owner; no historical adoption.
func (g *RuntimeRecoveryPreparedReleaseGuard) RetentionProof(ctx context.Context) (RuntimeRecoveryRetentionProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryRetentionProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryRetentionProof{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.retentionProofLocked(ctx)
}

func (g *RuntimeRecoveryPreparedReleaseGuard) PinRetentionProof(ctx context.Context) (RuntimeRecoveryRetentionProof, error) {
	return g.pinRetentionProof(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) pinRetentionProof(ctx context.Context, checkpoint func() error) (RuntimeRecoveryRetentionProof, error) {
	var zero RuntimeRecoveryRetentionProof
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	p, e := m.retentionProofLocked(ctx)
	if e != nil {
		return zero, e
	}
	if m.retentionSubmission != nil {
		return p, nil
	}
	deny := func() (RuntimeRecoveryRetentionProof, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	wire, e := json.Marshal(p)
	if e != nil || len(wire) > 8192 || !m.terminalAuthorityCurrent(ctx) || writeRemovalFileNoReplace(filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile), wire) != nil {
		return deny()
	}
	if checkpoint != nil && checkpoint() != nil {
		return deny()
	}
	m.retentionSubmission, m.retentionFrozen = wire, p
	if m.pinPreparedInventory() != nil {
		return deny()
	}
	if _, e = m.retentionProofLocked(ctx); e != nil {
		return deny()
	}
	return p, nil
}

func (g *RuntimeRecoveryPreparedReleaseGuard) RecordRetentionReceipt(ctx context.Context, receipt RuntimeRecoveryRetentionReceipt) error {
	return g.recordRetentionReceipt(ctx, receipt, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) recordRetentionReceipt(ctx context.Context, receipt RuntimeRecoveryRetentionReceipt, checkpoint func() error) error {
	if g == nil || g.state == nil {
		return ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return ErrRuntimeMutationBlocked
	}
	if _, e := m.retentionProofLocked(ctx); e != nil || m.retentionSubmission == nil || !retentionReceiptMatches(receipt, m.retentionFrozen, m.retentionSubmission) {
		return ErrRuntimeMutationBlocked
	}
	wire, e := json.Marshal(receipt)
	if e != nil || len(wire) > 4096 {
		return ErrRuntimeMutationBlocked
	}
	if m.retentionReceipt != nil {
		if !bytes.Equal(wire, m.retentionReceipt) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}
	deny := func() error { m.poisoned = true; return ErrRuntimeMutationBlocked }
	if !m.terminalAuthorityCurrent(ctx) || writeRemovalFileNoReplace(filepath.Join(m.dir, runtimeRecoveryRetentionAcknowledgementFile), wire) != nil {
		return deny()
	}
	if checkpoint != nil && checkpoint() != nil {
		return deny()
	}
	m.retentionReceipt = wire
	if m.pinPreparedInventory() != nil {
		return deny()
	}
	_, e = m.retentionProofLocked(ctx)
	return e
}
