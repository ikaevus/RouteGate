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

const RuntimeRecoveryAdmissionReleasePurpose = "runtime_mutation_admission_release_evidence_v1"

// A frozen Agent statement after local unlink/fsync and fresh runtime checks.
// It is not a Manager receipt, current admission state, or new mutation authority.
type RuntimeRecoveryAdmissionReleaseProof struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	AuthorizationHash         string                     `json:"authorizationHash"`
	CleanedProofHash          string                     `json:"cleanedProofHash"`
	CleanedReceiptHash        string                     `json:"cleanedReceiptHash"`
	CleanedArchiveHash        string                     `json:"cleanedArchiveHash"`
	ReleasedAt                time.Time                  `json:"releasedAt"`
	ObservedAt                time.Time                  `json:"observedAt"`
	Observation               RuntimeRecoveryObservation `json:"observation"`
}

func admissionReleaseProofPath(dir, operation string) (string, error) {
	if _, e := recoveryHistoryArchivePath(dir, operation); e != nil {
		return "", e
	}
	return filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-admission-proof-"+operation+".json"), nil
}

func admissionProofRecordCurrent(f *releaseRetentionSource) bool {
	if f == nil {
		return true
	}
	before, e := os.Lstat(f.path)
	data, e2 := readTerminalConfirmationFile(f.path, f.limit)
	after, e3 := os.Lstat(f.path)
	return e == nil && e2 == nil && e3 == nil && os.SameFile(f.info, before) && os.SameFile(f.info, after) && bytes.Equal(data, f.data)
}
func (m *RuntimeRecoveryFinalization) admissionProofUnchanged() bool {
	return m.admissionRelease == nil || admissionProofRecordCurrent(m.admissionRelease.proof)
}

func (m *RuntimeRecoveryFinalization) admissionProofLocked(ctx context.Context) (RuntimeRecoveryAdmissionReleaseProof, error) {
	var zero RuntimeRecoveryAdmissionReleaseProof
	s := m.admissionRelease
	if s == nil || s.result.Outcome != RuntimeRecoveryAdmissionDurable || s.removed != 5 || s.completedAt.IsZero() || !m.admissionReleaseCurrent(ctx) {
		return zero, ErrRuntimeMutationBlocked
	}
	at := m.lastChecked
	if s.proof != nil {
		at = s.frozen.ObservedAt
	}
	p := RuntimeRecoveryAdmissionReleaseProof{1, RuntimeRecoveryAdmissionReleasePurpose, m.authorization.OperationID, m.binding.AgentCredentialGeneration,
		m.authorizationHash, markerDigest(m.cleanedCommitSubmission), markerDigest(m.cleanedCommitReceipt), markerDigest(m.cleanedCommitArchive), s.completedAt, at, m.authorization.Expected}
	if p.ReleasedAt.Before(m.cleanedCommitFrozen.ObservedAt) || p.ObservedAt.Before(p.ReleasedAt) || p.ObservedAt.After(m.lastChecked) || !p.ObservedAt.Before(m.authorization.ExpiresAt) {
		return zero, ErrRuntimeMutationBlocked
	}
	wire, e := json.Marshal(p)
	if e != nil || s.proof != nil && (p != s.frozen || !bytes.Equal(wire, s.proof.data)) {
		return zero, ErrRuntimeMutationBlocked
	}
	return p, nil
}

// Only the same still-live owner may pin/retry within its release callback.
// The file is a private sibling outside the admission root and both immutable
// archives. It cannot itself block/reopen admission or reconstruct an owner.
func (g *RuntimeRecoveryPreparedReleaseGuard) PinAdmissionReleaseProof(ctx context.Context) (RuntimeRecoveryAdmissionReleaseProof, error) {
	return g.pinAdmissionReleaseProof(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) pinAdmissionReleaseProof(ctx context.Context, checkpoint func(string) error) (RuntimeRecoveryAdmissionReleaseProof, error) {
	var zero RuntimeRecoveryAdmissionReleaseProof
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.self != m {
		return zero, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoveryAdmissionReleaseProof, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	p, e := m.admissionProofLocked(ctx)
	if e != nil {
		return deny()
	}
	s := m.admissionRelease
	if s.proof != nil {
		return p, nil
	}
	path, e := admissionReleaseProofPath(m.dir, p.OperationID)
	if e != nil {
		return deny()
	}
	wire, e := json.Marshal(p)
	if e != nil || len(wire) > 8192 {
		return deny()
	}
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	if !cut("before") || !m.admissionReleaseCurrent(ctx) {
		return deny()
	}
	var pinned *os.File
	defer func() {
		if pinned != nil {
			pinned.Close()
		}
	}()
	e = publishRemovalFileCheckpoint(path, wire, false, func(step string, f *os.File) error {
		if step == "file" {
			fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
			if errno != 0 {
				return ErrRuntimeMutationBlocked
			}
			pinned = os.NewFile(fd, "admission-proof")
		}
		if !cut(step) || !m.admissionReleaseCurrent(ctx) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
	if e != nil || !cut("published") || pinned == nil {
		return deny()
	}
	info, e := pinned.Stat()
	if e != nil {
		return deny()
	}
	record := &releaseRetentionSource{path: path, limit: 8192, info: info, data: wire}
	if !admissionProofRecordCurrent(record) || !m.admissionReleaseCurrent(ctx) {
		return deny()
	}
	s.proof, s.frozen = record, p
	if !cut("done") || !m.admissionReleaseCurrent(ctx) {
		return deny()
	}
	return p, nil
}

func admissionProofMatchesHistory(p RuntimeRecoveryAdmissionReleaseProof, b RuntimeRecoveryGrantBinding, h RuntimeRecoveryCleanedCommitArchivedHistory) bool {
	proof, _ := json.Marshal(h.Proof)
	receipt, _ := json.Marshal(h.Receipt)
	return p.SchemaVersion == 1 && p.Purpose == RuntimeRecoveryAdmissionReleasePurpose && p.OperationID == h.Proof.OperationID &&
		p.AgentCredentialGeneration == b.AgentCredentialGeneration && p.AgentCredentialGeneration == h.Proof.AgentCredentialGeneration &&
		p.AuthorizationHash == h.Proof.AuthorizationHash && p.CleanedProofHash == markerDigest(proof) &&
		p.CleanedReceiptHash == markerDigest(receipt) && p.CleanedArchiveHash == h.ManifestHash && p.Observation == h.Proof.Observation &&
		!p.ReleasedAt.IsZero() && !p.ReleasedAt.Before(h.Proof.ObservedAt) && !p.ObservedAt.Before(p.ReleasedAt) &&
		p.ObservedAt.Before(h.Intent.ExpiresAt)
}

// Exact historical statement under the ORIGINAL flock and full archive chain.
// This does not inspect current runtime/admission, repair files, or renew authority.
func WithRecoveryAdmissionReleaseHistory(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryAdmissionReleaseProof) error) error {
	if fn == nil {
		return ErrRuntimeMutationBlocked
	}
	path, e := admissionReleaseProofPath(dir, operation)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	return WithArchivedRecoveryCleanedCommit(ctx, dir, operation, b, func(h RuntimeRecoveryCleanedCommitArchivedHistory) error {
		info, e := os.Lstat(path)
		data, e2 := readTerminalConfirmationFile(path, 8192)
		var p RuntimeRecoveryAdmissionReleaseProof
		record := &releaseRetentionSource{path: path, limit: 8192, info: info, data: data}
		if e != nil || e2 != nil || !exactHistoricalJSON(data, &p) || !admissionProofMatchesHistory(p, b, h) || !admissionProofRecordCurrent(record) || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		if fn(p) != nil || ctx.Err() != nil || !admissionProofRecordCurrent(record) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}
