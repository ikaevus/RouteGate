package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

// Local read-only snapshot, never an executable deletion list or release grant.
// No API accepts this value as authority. A future executor must derive its own
// fresh locked state and preserve both admission barriers and the stable lock.
type RuntimeRecoverySourceCleanupPlan struct {
	OperationID, AuthorizationHash, ManifestHash, RetentionProofHash, RetentionReceiptHash string
	Binding                                                                                RuntimeRecoveryGrantBinding
	ExpiresAt                                                                              time.Time
	IndependentArchiveName, OriginalArchiveName, AdmissionLockName                         string
	RootBarriers                                                                           [2]RuntimeRecoveryFenceReleaseFile
	Candidates                                                                             []RuntimeRecoverySourceCleanupFile
}

type RuntimeRecoverySourceCleanupFile struct {
	Scope, Name, Hash string
}

// The original live callback-scoped owner must have created and acknowledged
// retention and published the independent archive. Historical readback cannot
// adopt this phase. Current Manager readiness is independently checked by Client.
func (g *RuntimeRecoveryPreparedReleaseGuard) PlanArchivedSourceCleanup(ctx context.Context) (RuntimeRecoverySourceCleanupPlan, error) {
	return g.planArchivedSourceCleanup(ctx, nil)
}

func (g *RuntimeRecoveryPreparedReleaseGuard) planArchivedSourceCleanup(ctx context.Context, checkpoint func() error) (RuntimeRecoverySourceCleanupPlan, error) {
	var zero RuntimeRecoverySourceCleanupPlan
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	return m.planArchivedSourceCleanupLocked(ctx, checkpoint)
}

func (m *RuntimeRecoveryFinalization) planArchivedSourceCleanupLocked(ctx context.Context, checkpoint func() error) (RuntimeRecoverySourceCleanupPlan, error) {
	var zero RuntimeRecoverySourceCleanupPlan
	if m.sourceCleanup != nil {
		return zero, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoverySourceCleanupPlan, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	if m.historyArchive == nil || m.historyArchiveUnchanged == nil || m.retentionReceipt == nil || m.retentionSubmission == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	p, err := m.retentionProofLocked(ctx)
	if err != nil {
		return deny()
	}
	path, err := recoveryHistoryArchivePath(m.dir, p.OperationID)
	if err != nil {
		return deny()
	}
	files, sourcesCurrent, err := collectRecoveryHistory(m.dir, p.OperationID)
	if err != nil {
		return deny()
	}
	var receipt RuntimeRecoveryRetentionReceipt
	if !exactHistoricalJSON(m.retentionReceipt, &receipt) || !retentionReceiptMatches(receipt, p, m.retentionSubmission) {
		return deny()
	}
	plan := RuntimeRecoverySourceCleanupPlan{
		OperationID: p.OperationID, AuthorizationHash: m.authorizationHash, ManifestHash: markerDigest(m.historyArchive),
		RetentionProofHash: markerDigest(m.retentionSubmission), RetentionReceiptHash: markerDigest(m.retentionReceipt),
		Binding: m.binding, ExpiresAt: m.authorization.ExpiresAt, IndependentArchiveName: filepath.Base(path),
		OriginalArchiveName: filepath.Base(m.archive), AdmissionLockName: "runtime.lock",
	}
	err = withRecoveryHistoryArchive(ctx, path, m.binding, p.OperationID, func(h RuntimeRecoveryRetentionHistory, hash string, archiveCurrent func() bool) error {
		if h.Proof != p || h.LocalReceipt != receipt || hash != plan.ManifestHash {
			return ErrRuntimeMutationBlocked
		}
		// Compare the complete current ORIGINAL inventory with the validated
		// independent copy. Neither a digest alone nor a redundant old receipt
		// can establish that the originals are safe cleanup candidates.
		expected, e := json.Marshal(historyArchiveRecord(m.binding, h, files))
		if e != nil || !bytes.Equal(expected, m.historyArchive) {
			return ErrRuntimeMutationBlocked
		}
		for _, f := range files {
			v := f.metadata
			if v.Scope == "root" && v.Name == "mutation-inflight.json" {
				plan.RootBarriers[0] = RuntimeRecoveryFenceReleaseFile{v.Name, v.Hash}
				continue
			}
			if v.Scope == "root" && v.Name == runtimeRecoveryReleaseIntentFile {
				plan.RootBarriers[1] = RuntimeRecoveryFenceReleaseFile{v.Name, v.Hash}
				continue
			}
			plan.Candidates = append(plan.Candidates, RuntimeRecoverySourceCleanupFile{v.Scope, v.Name, v.Hash})
		}
		if plan.RootBarriers[0].Name == "" || plan.RootBarriers[1].Name == "" {
			return ErrRuntimeMutationBlocked
		}
		if checkpoint != nil && checkpoint() != nil {
			return ErrRuntimeMutationBlocked
		}
		fresh, e := m.retentionProofLocked(ctx)
		if e != nil || fresh != p || !sourcesCurrent() || !archiveCurrent() || !m.historyArchiveUnchanged() || !m.terminalAuthorityCurrent(ctx) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
	if err != nil || !sourcesCurrent() || !m.historyArchiveUnchanged() || !m.terminalAuthorityCurrent(ctx) {
		return deny()
	}
	return plan, nil
}
