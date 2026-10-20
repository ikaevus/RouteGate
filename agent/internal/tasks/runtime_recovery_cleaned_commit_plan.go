package tasks

import (
	"context"
	"path/filepath"
	"time"
)

// A point-in-time read-only inventory. Nothing consumes this value as deletion
// authority. A future commit must derive its own inventory under the live lock.
type RuntimeRecoveryCleanedCommitPlan struct {
	OperationID, AuthorizationHash, ProofHash, ReceiptHash string
	Binding                                                RuntimeRecoveryGrantBinding
	ObservedAt, ExpiresAt                                  time.Time
	AdmissionLockName, HistoryArchiveName, ManifestHash    string
	Files                                                  [5]RuntimeRecoveryFenceReleaseFile
}

func (m *RuntimeRecoveryFinalization) acknowledgedCleanedCommitLocked(ctx context.Context) (RuntimeRecoveryCleanedCommitProof, RuntimeRecoveryCleanedCommitReceipt, error) {
	var zero RuntimeRecoveryCleanedCommitProof
	var receipt RuntimeRecoveryCleanedCommitReceipt
	if m.cleanedCommitSubmission == nil || m.cleanedCommitReceipt == nil {
		return zero, receipt, ErrRuntimeMutationBlocked
	}
	p, e := m.cleanedCommitProofLocked(ctx)
	if e != nil {
		return zero, receipt, e
	}
	if !exactHistoricalJSON(m.cleanedCommitReceipt, &receipt) || !cleanedCommitReceiptMatches(receipt, p, m.cleanedCommitSubmission) {
		m.poisoned = true
		return zero, RuntimeRecoveryCleanedCommitReceipt{}, ErrRuntimeMutationBlocked
	}
	return p, receipt, nil
}

// Return only the original owner's durable proof AND exact stored receipt,
// after fresh locked runtime/history checks. Historical bytes cannot enter here.
func (g *RuntimeRecoveryPreparedReleaseGuard) AcknowledgedCleanedCommit(ctx context.Context) (RuntimeRecoveryCleanedCommitProof, RuntimeRecoveryCleanedCommitReceipt, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryCleanedCommitProof{}, RuntimeRecoveryCleanedCommitReceipt{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryCleanedCommitProof{}, RuntimeRecoveryCleanedCommitReceipt{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.acknowledgedCleanedCommitLocked(ctx)
}

// Both root barriers, the intent and the proof/ack form the fixed remaining
// set. The existing lock and independent history are explicitly protected.
func (g *RuntimeRecoveryPreparedReleaseGuard) PlanCleanedCommit(ctx context.Context) (RuntimeRecoveryCleanedCommitPlan, error) {
	var zero RuntimeRecoveryCleanedCommitPlan
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	p, r, e := m.acknowledgedCleanedCommitLocked(ctx)
	if e != nil {
		return zero, e
	}
	deny := func() (RuntimeRecoveryCleanedCommitPlan, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	s := m.sourceCleanup
	if s == nil || !s.status().SourcesRemoved || s.commitIntent == nil || s.commitSubmission == nil || s.commitReceipt == nil {
		return deny()
	}
	archive, e := recoveryHistoryArchivePath(m.dir, p.OperationID)
	if e != nil {
		return deny()
	}
	plan := RuntimeRecoveryCleanedCommitPlan{OperationID: p.OperationID, AuthorizationHash: m.authorizationHash,
		ProofHash: markerDigest(m.cleanedCommitSubmission), ReceiptHash: markerDigest(m.cleanedCommitReceipt),
		Binding: m.binding, ExpiresAt: m.authorization.ExpiresAt, AdmissionLockName: "runtime.lock",
		HistoryArchiveName: filepath.Base(archive), ManifestHash: p.ManifestHash}
	owned := map[string][]byte{}
	for _, barrier := range s.barriers {
		owned[filepath.Base(barrier.path)] = barrier.data
	}
	owned[runtimeRecoveryCleanedCommitIntentFile] = s.commitIntent.data
	owned[runtimeRecoveryCleanedCommitSubmissionFile] = s.commitSubmission.data
	owned[runtimeRecoveryCleanedCommitAcknowledgementFile] = s.commitReceipt.data
	names := [5]string{"mutation-inflight.json", runtimeRecoveryReleaseIntentFile, runtimeRecoveryCleanedCommitIntentFile, runtimeRecoveryCleanedCommitSubmissionFile, runtimeRecoveryCleanedCommitAcknowledgementFile}
	if len(owned) != len(names) {
		return deny()
	}
	for i, name := range names {
		if len(owned[name]) == 0 {
			return deny()
		}
		plan.Files[i] = RuntimeRecoveryFenceReleaseFile{name, markerDigest(owned[name])}
	}
	if plan.Files[0].Hash != p.MarkerHash || plan.Files[1].Hash != p.ReleaseIntentHash || plan.Files[2].Hash != p.CleanedIntentHash || plan.Files[3].Hash != r.ProofHash {
		return deny()
	}
	// Reobserve after constructing the inventory; retain the ORIGINAL snapshots
	// so replacing any same-byte file between observations cannot be adopted.
	fresh, ack, e := m.acknowledgedCleanedCommitLocked(ctx)
	if e != nil || fresh != p || ack != r || !s.unchanged() {
		return deny()
	}
	plan.ObservedAt = m.lastChecked
	return plan, nil
}
