package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const runtimeRecoveryCleanedCommitSubmissionFile = "recovery-cleaned-commit-submission.json"
const runtimeRecoveryCleanedCommitAcknowledgementFile = "recovery-cleaned-commit-acknowledgement.json"

func ownedCleanupRecord(s *releaseRetentionSource, data []byte) bool {
	return (s == nil && data == nil) || s != nil && data != nil && bytes.Equal(s.data, data)
}
func cleanedCommitProof(p RuntimeRecoveryCleanedCommitIntent, wire []byte, at time.Time) RuntimeRecoveryCleanedCommitProof {
	return RuntimeRecoveryCleanedCommitProof{1, RuntimeRecoveryCleanedCommitPurpose, p.OperationID,
		p.Binding.AgentCredentialGeneration, p.AuthorizationHash, p.RetentionProofHash, p.RetentionReceiptHash,
		p.ReleaseIntentHash, markerDigest(wire), p.ManifestHash, p.MarkerHash, p.ObservedAt, at, p.Observation}
}
func cleanedCommitReceiptMatches(r RuntimeRecoveryCleanedCommitReceipt, p RuntimeRecoveryCleanedCommitProof, wire []byte) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID && r.CleanedIntentHash == p.CleanedIntentHash && r.ProofHash == markerDigest(wire) && r.RequestHash == r.ProofHash
}

func (m *RuntimeRecoveryFinalization) cleanedCommitProofLocked(ctx context.Context) (RuntimeRecoveryCleanedCommitProof, error) {
	var zero RuntimeRecoveryCleanedCommitProof
	if m.cleanedCommitIntent == nil || !m.sourceCleanupCurrent(ctx) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	var intent RuntimeRecoveryCleanedCommitIntent
	if !exactHistoricalJSON(m.cleanedCommitIntent, &intent) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	at := m.lastChecked
	if m.cleanedCommitSubmission != nil {
		at = m.cleanedCommitFrozen.ObservedAt
	}
	p := cleanedCommitProof(intent, m.cleanedCommitIntent, at)
	wire, e := json.Marshal(p)
	if e != nil || at.Before(intent.ObservedAt) || at.After(m.lastChecked) || !at.Before(intent.ExpiresAt) || m.cleanedCommitSubmission != nil && (p != m.cleanedCommitFrozen || !bytes.Equal(wire, m.cleanedCommitSubmission)) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	return p, nil
}

func (g *RuntimeRecoveryPreparedReleaseGuard) CleanedCommitProof(ctx context.Context) (RuntimeRecoveryCleanedCommitProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryCleanedCommitProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryCleanedCommitProof{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.cleanedCommitProofLocked(ctx)
}

// Publication pins the writer inode until the exact new inventory is captured.
// No existing file, even identical bytes, is adopted by a live owner.
func (m *RuntimeRecoveryFinalization) publishCleanedCommitRecord(ctx context.Context, name string, data []byte, checkpoint func(string) error) error {
	deny := func() error { m.poisoned = true; return ErrRuntimeMutationBlocked }
	if len(data) > 8192 || !m.sourceCleanupCurrent(ctx) {
		return deny()
	}
	var published *os.File
	defer func() {
		if published != nil {
			published.Close()
		}
	}()
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	if !cut("before") || !m.sourceCleanupCurrent(ctx) {
		return deny()
	}
	err := publishRemovalFileCheckpoint(filepath.Join(m.dir, name), data, false, func(step string, file *os.File) error {
		if step == "file" {
			fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
			if errno != 0 {
				return ErrRuntimeMutationBlocked
			}
			published = os.NewFile(fd, name)
		}
		if !cut(step) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
	if err != nil || !cut("published") {
		return deny()
	}
	s := m.sourceCleanup
	n, err := readRecoverySourceCleanup(m.dir, s.operation, m.lock, s.files, s.hash)
	if err != nil || published == nil || !sameCleanupInfo(s.root, n.root) || !sameCleanupInfo(s.lockInfo, n.lockInfo) || !sameCleanupRecord(s.commitIntent, n.commitIntent) {
		return deny()
	}
	for i := range s.barriers {
		if !sameCleanupInfo(s.barriers[i].info, n.barriers[i].info) {
			return deny()
		}
	}
	var record *releaseRetentionSource
	if name == runtimeRecoveryCleanedCommitSubmissionFile {
		if !sameCleanupRecord(s.commitReceipt, n.commitReceipt) {
			return deny()
		}
		record = n.commitSubmission
	} else {
		if !sameCleanupRecord(s.commitSubmission, n.commitSubmission) {
			return deny()
		}
		record = n.commitReceipt
	}
	info, err := published.Stat()
	if err != nil || record == nil || !os.SameFile(info, record.info) || !bytes.Equal(record.data, data) {
		return deny()
	}
	m.sourceCleanup = n
	if name == runtimeRecoveryCleanedCommitSubmissionFile {
		m.cleanedCommitSubmission = data
	} else {
		m.cleanedCommitReceipt = data
	}
	if !m.sourceCleanupCurrent(ctx) || !cut("done") || !m.sourceCleanupCurrent(ctx) {
		return deny()
	}
	return nil
}

// Freeze a durable public statement before HTTP, without changing either fence.
func (g *RuntimeRecoveryPreparedReleaseGuard) PinCleanedCommitProof(ctx context.Context) (RuntimeRecoveryCleanedCommitProof, error) {
	return g.pinCleanedCommitProof(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) pinCleanedCommitProof(ctx context.Context, checkpoint func(string) error) (RuntimeRecoveryCleanedCommitProof, error) {
	var zero RuntimeRecoveryCleanedCommitProof
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	p, e := m.cleanedCommitProofLocked(ctx)
	if e != nil {
		return zero, e
	}
	if m.cleanedCommitSubmission != nil {
		return p, nil
	}
	wire, e := json.Marshal(p)
	if e != nil {
		return zero, ErrRuntimeMutationBlocked
	}
	m.cleanedCommitFrozen = p
	if m.publishCleanedCommitRecord(ctx, runtimeRecoveryCleanedCommitSubmissionFile, wire, checkpoint) != nil {
		return zero, ErrRuntimeMutationBlocked
	}
	return p, nil
}

func (g *RuntimeRecoveryPreparedReleaseGuard) RecordCleanedCommitReceipt(ctx context.Context, r RuntimeRecoveryCleanedCommitReceipt) error {
	return g.recordCleanedCommitReceipt(ctx, r, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) recordCleanedCommitReceipt(ctx context.Context, r RuntimeRecoveryCleanedCommitReceipt, checkpoint func(string) error) error {
	if g == nil || g.state == nil {
		return ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return ErrRuntimeMutationBlocked
	}
	p, e := m.cleanedCommitProofLocked(ctx)
	if e != nil || m.cleanedCommitSubmission == nil || !cleanedCommitReceiptMatches(r, p, m.cleanedCommitSubmission) {
		return ErrRuntimeMutationBlocked
	}
	wire, e := json.Marshal(r)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	if m.cleanedCommitReceipt != nil {
		if bytes.Equal(wire, m.cleanedCommitReceipt) {
			return nil
		}
		return ErrRuntimeMutationBlocked
	}
	return m.publishCleanedCommitRecord(ctx, runtimeRecoveryCleanedCommitAcknowledgementFile, wire, checkpoint)
}

// Historical storage reconciliation never creates an ack or adopts an owner.
func WithRecoveryCleanedCommitEvidenceHistory(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryCleanedCommitProof, RuntimeRecoveryCleanedCommitReceipt) error) error {
	if fn == nil {
		return ErrRuntimeMutationBlocked
	}
	return withRecoverySourceCleanupHistory(ctx, dir, operation, b, func(s *recoverySourceCleanupState, _ RuntimeRecoveryRetentionHistory) error {
		var p RuntimeRecoveryCleanedCommitProof
		var r RuntimeRecoveryCleanedCommitReceipt
		var intent RuntimeRecoveryCleanedCommitIntent
		if s.commitIntent == nil || !exactHistoricalJSON(s.commitIntent.data, &intent) || intent.Binding != b || s.commitSubmission == nil || !exactHistoricalJSON(s.commitSubmission.data, &p) {
			return ErrRuntimeMutationBlocked
		}
		if s.commitReceipt != nil && !exactHistoricalJSON(s.commitReceipt.data, &r) {
			return ErrRuntimeMutationBlocked
		}
		return fn(p, r)
	})
}
