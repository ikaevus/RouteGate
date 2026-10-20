package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const runtimeRecoveryPreparedSubmissionFile = "recovery-prepared-release-submission.json"
const runtimeRecoveryPreparedAcknowledgementFile = "recovery-prepared-release-acknowledgement.json"

func preparedReleaseStateAbsent(dir string) bool {
	if !runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryCleanedCommitSubmissionFile)) || !runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryCleanedCommitAcknowledgementFile)) {
		return false
	}
	return runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryCleanedCommitIntentFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryPreparedSubmissionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryPreparedAcknowledgementFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryReleaseRetentionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryRetentionSubmissionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryRetentionAcknowledgementFile))
}
func preparedReleaseReceiptMatches(r RuntimeRecoveryPreparedReleaseReceipt, p RuntimeRecoveryPreparedReleaseProof, wire []byte) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID && r.ReleaseIntentHash == p.ReleaseIntentHash && r.ProofHash == markerDigest(wire) && r.RequestHash == r.ProofHash
}

// Rechecks the original live authority, complete inventory and real runtime.
// Once pinned, ObservedAt stays frozen; reobservation never renews that proof.
func (m *RuntimeRecoveryFinalization) preparedReleaseProofLocked(ctx context.Context) (RuntimeRecoveryPreparedReleaseProof, error) {
	if m.sourceCleanup != nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoveryPreparedReleaseProof, error) {
		m.poisoned = true
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	if m.releaseIntent == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	if m.historyArchiveUnchanged != nil && !m.historyArchiveUnchanged() {
		return deny()
	}
	if m.preparedUnchanged != nil && !m.preparedUnchanged() {
		return deny()
	}
	if m.retentionUnchanged != nil && !m.retentionUnchanged() {
		return deny()
	}
	p, e := m.planFenceReleaseLocked(ctx, m.binding, m.authorizationHash)
	if e != nil {
		return deny()
	}
	var intent runtimeRecoveryReleaseIntent
	if !exactHistoricalJSON(m.releaseIntent, &intent) || intent != releaseIntentForPlan(p, intent.PreparedAt) || intent.PreparedAt.Before(m.terminalFrozen.ObservedAt) || intent.PreparedAt.After(m.lastChecked) || !intent.PreparedAt.Before(p.ExpiresAt) {
		return deny()
	}
	proof := RuntimeRecoveryPreparedReleaseProof{SchemaVersion: 1, Purpose: RuntimeRecoveryPreparedReleasePurpose, OperationID: p.OperationID, AgentCredentialGeneration: m.binding.AgentCredentialGeneration, AuthorizationHash: p.AuthorizationHash, TerminalProofHash: p.TerminalProofHash, TerminalReceiptHash: p.TerminalReceiptHash, ReleaseIntentHash: markerDigest(m.releaseIntent), PreparedAt: intent.PreparedAt, ObservedAt: m.lastChecked, Observation: m.terminalFrozen.Observation}
	if m.preparedSubmission != nil {
		proof.ObservedAt = m.preparedFrozen.ObservedAt
		wire, e := json.Marshal(proof)
		if e != nil || proof != m.preparedFrozen || !bytes.Equal(wire, m.preparedSubmission) || proof.ObservedAt.Before(proof.PreparedAt) || proof.ObservedAt.After(m.lastChecked) || !proof.ObservedAt.Before(p.ExpiresAt) {
			return deny()
		}
	}
	if m.preparedUnchanged != nil && !m.preparedUnchanged() {
		return deny()
	}
	if m.releaseHistoryRetention != nil {
		if _, e := validateReleaseHistoryRetention(m.releaseHistoryRetention, m.dir, m.archive, m.binding, proof, m.authorization.ExpiresAt); e != nil {
			return deny()
		}
	}
	if m.retentionUnchanged != nil && !m.retentionUnchanged() {
		return deny()
	}
	if m.releaseHistoryRetention != nil && !m.terminalAuthorityCurrent(ctx) {
		return deny()
	}
	if m.historyArchiveUnchanged != nil && !m.historyArchiveUnchanged() {
		return deny()
	}
	return proof, nil
}

type runtimeRecoveryPreparedInventory struct {
	files         [11]releasePlanFileSnapshot
	root, archive os.FileInfo
}

// Snapshot all owned files/inodes, including confirmation files, across HTTP.
// Only successful no-replace publication may advance this private snapshot.
func (m *RuntimeRecoveryFinalization) pinPreparedInventory() error {
	inventory := fenceReleaseInventory(m.dir, m.archive, m.lock, m.releaseIntent, m.preparedSubmission, m.preparedAcknowledgement, m.releaseHistoryRetention, m.retentionSubmission, m.retentionReceipt)
	before, root, arch, e := inventory()
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	if old := m.preparedInventory; old != nil {
		if !os.SameFile(old.root, root) || !os.SameFile(old.archive, arch) {
			return ErrRuntimeMutationBlocked
		}
		for i := range old.files {
			if old.files[i].info == nil {
				continue
			}
			if before[i].info == nil || old.files[i].file != before[i].file || !os.SameFile(old.files[i].info, before[i].info) {
				return ErrRuntimeMutationBlocked
			}
		}
	}
	m.preparedInventory = &runtimeRecoveryPreparedInventory{before, root, arch}
	m.preparedUnchanged = func() bool {
		after, r, a, e := inventory()
		if e != nil || !os.SameFile(root, r) || !os.SameFile(arch, a) {
			return false
		}
		for i := range before {
			if before[i].info == nil && after[i].info == nil {
				continue
			}
			if before[i].info == nil || after[i].info == nil || before[i].file != after[i].file || !os.SameFile(before[i].info, after[i].info) {
				return false
			}
		}
		return true
	}
	return nil
}

type RuntimeRecoveryPreparedReleaseGuard struct {
	state *runtimeRecoveryPreparedReleaseGuardState
}
type runtimeRecoveryPreparedReleaseGuardState struct {
	mu    sync.Mutex
	owner *RuntimeRecoveryFinalization
}

func (g *RuntimeRecoveryPreparedReleaseGuard) Proof(ctx context.Context) (RuntimeRecoveryPreparedReleaseProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.preparedReleaseProofLocked(ctx)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) PinProof(ctx context.Context) (RuntimeRecoveryPreparedReleaseProof, error) {
	return g.pinProof(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) pinProof(ctx context.Context, checkpoint func() error) (RuntimeRecoveryPreparedReleaseProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	deny := func() (RuntimeRecoveryPreparedReleaseProof, error) {
		m.poisoned = true
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	p, e := m.preparedReleaseProofLocked(ctx)
	if e != nil {
		return RuntimeRecoveryPreparedReleaseProof{}, e
	}
	if m.preparedSubmission != nil {
		return p, nil
	}
	data, e := json.Marshal(p)
	if e != nil || len(data) > 8192 || !m.terminalAuthorityCurrent(ctx) || writeRemovalFileNoReplace(filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile), data) != nil {
		return deny()
	}
	// Test checkpoint after fsync, before memory adoption. Crash retains a fence.
	if checkpoint != nil && checkpoint() != nil {
		return deny()
	}
	m.preparedSubmission = data
	m.preparedFrozen = p
	if m.pinPreparedInventory() != nil {
		return deny()
	}
	if _, e = m.preparedReleaseProofLocked(ctx); e != nil {
		return deny()
	}
	return p, nil
}
func (g *RuntimeRecoveryPreparedReleaseGuard) RecordReceipt(ctx context.Context, r RuntimeRecoveryPreparedReleaseReceipt) error {
	return g.recordReceipt(ctx, r, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) recordReceipt(ctx context.Context, r RuntimeRecoveryPreparedReleaseReceipt, checkpoint func() error) error {
	if g == nil || g.state == nil {
		return ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	if _, e := m.preparedReleaseProofLocked(ctx); e != nil {
		return e
	}
	if m.preparedSubmission == nil || !preparedReleaseReceiptMatches(r, m.preparedFrozen, m.preparedSubmission) {
		return ErrRuntimeMutationBlocked
	}
	data, e := json.Marshal(r)
	if e != nil || len(data) > 4096 {
		return ErrRuntimeMutationBlocked
	}
	if m.preparedAcknowledgement != nil {
		if !bytes.Equal(data, m.preparedAcknowledgement) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}
	deny := func() error { m.poisoned = true; return ErrRuntimeMutationBlocked }
	if !m.terminalAuthorityCurrent(ctx) || writeRemovalFileNoReplace(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile), data) != nil {
		return deny()
	}
	if checkpoint != nil && checkpoint() != nil {
		return deny()
	}
	m.preparedAcknowledgement = data
	if m.pinPreparedInventory() != nil {
		return deny()
	}
	_, e = m.preparedReleaseProofLocked(ctx)
	return e
}

// Original live owner only. Mutex/flock span pin, HTTP, receipt durability and
// fresh final runtime validation. Escaped/copy guards expire together. No release.
func (m *RuntimeRecoveryFinalization) WithPreparedReleaseSubmission(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string, fn func(*RuntimeRecoveryPreparedReleaseGuard) error) error {
	if m == nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binding != b || m.authorizationHash != authorizationHash || m.releaseIntent == nil {
		return ErrRuntimeMutationBlocked
	}
	g := &RuntimeRecoveryPreparedReleaseGuard{state: &runtimeRecoveryPreparedReleaseGuardState{owner: m}}
	defer func() { g.state.mu.Lock(); g.state.owner = nil; g.state.mu.Unlock() }()
	if _, e := g.Proof(ctx); e != nil {
		return e
	}
	if m.preparedUnchanged == nil && m.pinPreparedInventory() != nil {
		m.poisoned = true
		return ErrRuntimeMutationBlocked
	}
	e := fn(g)
	g.state.mu.Lock()
	var final error
	if m.sourceCleanup != nil {
		if !m.sourceCleanup.status().SourcesRemoved || !m.sourceCleanupCurrent(ctx) {
			m.poisoned = true
			final = ErrRuntimeMutationBlocked
		}
	} else {
		_, final = m.preparedReleaseProofLocked(ctx)
	}
	g.state.mu.Unlock()
	if final != nil {
		return final
	}
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	return nil
}
