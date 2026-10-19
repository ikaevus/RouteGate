package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const runtimeRecoveryCleanedCommitIntentFile = "recovery-cleaned-commit-intent.json"
const runtimeRecoveryCleanedCommitIntentPurpose = "runtime_mutation_cleaned_commit_intent_v1"

// Private local commitment, not a grant or proof of Manager completion, fsync
// completion after a crash, fence release or working VPN traffic. No consumer
// may adopt a historical intent as a live owner or resume barrier removal.
type RuntimeRecoveryCleanedCommitIntent struct {
	SchemaVersion        int                         `json:"schemaVersion"`
	Purpose              string                      `json:"purpose"`
	OperationID          string                      `json:"operationId"`
	Binding              RuntimeRecoveryGrantBinding `json:"binding"`
	AuthorizationHash    string                      `json:"authorizationHash"`
	ManifestHash         string                      `json:"manifestHash"`
	RetentionProofHash   string                      `json:"retentionProofHash"`
	RetentionReceiptHash string                      `json:"retentionReceiptHash"`
	MarkerHash           string                      `json:"markerHash"`
	ReleaseIntentHash    string                      `json:"releaseIntentHash"`
	ObservedAt           time.Time                   `json:"observedAt"`
	ExpiresAt            time.Time                   `json:"expiresAt"`
	Observation          RuntimeRecoveryObservation  `json:"observation"`
}

// Derive every binding/digest from the already validated independent inventory,
// never from a caller-supplied cleanup snapshot or candidate list.
func (s *recoverySourceCleanupState) cleanedCommitIntent(at time.Time) (RuntimeRecoveryCleanedCommitIntent, error) {
	var zero RuntimeRecoveryCleanedCommitIntent
	if !s.status().SourcesRemoved || at.IsZero() {
		return zero, ErrRuntimeMutationBlocked
	}
	root := map[string][]byte{}
	for _, f := range s.files {
		if f.metadata.Scope == "root" {
			root[f.metadata.Name] = f.data
		}
	}
	var prepared runtimeRecoveryReleaseIntent
	var proof RuntimeRecoveryRetentionProof
	var receipt RuntimeRecoveryRetentionReceipt
	if !exactHistoricalJSON(root[runtimeRecoveryReleaseIntentFile], &prepared) || !exactHistoricalJSON(root[runtimeRecoveryRetentionSubmissionFile], &proof) || !exactHistoricalJSON(root[runtimeRecoveryRetentionAcknowledgementFile], &receipt) || len(root["mutation-inflight.json"]) == 0 || prepared.OperationID != s.operation || proof.OperationID != s.operation || prepared.AuthorizationHash != proof.AuthorizationHash || at.Before(proof.ObservedAt) || at.Before(prepared.PreparedAt) || !at.Before(prepared.ExpiresAt) || !retentionReceiptMatches(receipt, proof, root[runtimeRecoveryRetentionSubmissionFile]) {
		return zero, ErrRuntimeMutationBlocked
	}
	return RuntimeRecoveryCleanedCommitIntent{1, runtimeRecoveryCleanedCommitIntentPurpose,
		s.operation, prepared.Binding, prepared.AuthorizationHash, s.hash,
		markerDigest(root[runtimeRecoveryRetentionSubmissionFile]), markerDigest(root[runtimeRecoveryRetentionAcknowledgementFile]),
		markerDigest(root["mutation-inflight.json"]), markerDigest(root[runtimeRecoveryReleaseIntentFile]),
		at, prepared.ExpiresAt, proof.Observation}, nil
}

// Called only inside the original callback-scoped cleanup owner. The Client
// must independently revalidate current credentials, original authorization
// and prepared/retention storage first. This adds one no-replace durable record;
// neither admission barrier nor independent history is modified or removed.
func (g *RuntimeRecoveryPreparedReleaseGuard) PrepareCleanedCommit(ctx context.Context) (RuntimeRecoveryCleanedCommitIntent, error) {
	return g.prepareCleanedCommit(ctx, nil)
}

func (g *RuntimeRecoveryPreparedReleaseGuard) prepareCleanedCommit(ctx context.Context, checkpoint func(string) error) (RuntimeRecoveryCleanedCommitIntent, error) {
	var zero RuntimeRecoveryCleanedCommitIntent
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.self != m || m.sourceCleanup == nil || !m.sourceCleanup.status().SourcesRemoved {
		return zero, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoveryCleanedCommitIntent, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	if !m.sourceCleanupCurrent(ctx) {
		return deny()
	}
	if m.cleanedCommitIntent != nil {
		var saved RuntimeRecoveryCleanedCommitIntent
		if !exactHistoricalJSON(m.cleanedCommitIntent, &saved) {
			return deny()
		}
		return saved, nil // Freeze time and bytes; never renew authority.
	}
	p, e := m.sourceCleanup.cleanedCommitIntent(m.lastChecked)
	if e != nil || p.Binding != m.binding || p.AuthorizationHash != m.authorizationHash || p.Observation != m.authorization.Expected || p.ExpiresAt != m.authorization.ExpiresAt {
		return deny()
	}
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	data, e := json.Marshal(p)
	var published *os.File
	defer func() {
		if published != nil {
			published.Close()
		}
	}()
	publishCut := func(step string, file *os.File) error {
		if step == "file" {
			fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
			if errno != 0 {
				return ErrRuntimeMutationBlocked
			}
			published = os.NewFile(fd, runtimeRecoveryCleanedCommitIntentFile)
		}
		if !cut(step) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}
	if e != nil || len(data) > 8192 || !cut("before") || !m.sourceCleanupCurrent(ctx) || publishRemovalFileCheckpoint(filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile), data, false, publishCut) != nil {
		return deny()
	}
	// Publication already fsyncs file and parent. Any later uncertainty keeps
	// this record and both barriers, poisons the owner and never auto-resumes.
	if !cut("published") {
		return deny()
	}
	s := m.sourceCleanup
	n, e := readRecoverySourceCleanup(m.dir, s.operation, m.lock, s.files, s.hash)
	if e != nil || n.commitIntent == nil || published == nil {
		return deny()
	}
	publishedInfo, e := published.Stat()
	if e != nil || !os.SameFile(publishedInfo, n.commitIntent.info) || !sameCleanupInfo(s.root, n.root) || !sameCleanupInfo(s.lockInfo, n.lockInfo) {
		return deny()
	}
	for i := range s.barriers {
		if !sameCleanupInfo(s.barriers[i].info, n.barriers[i].info) {
			return deny()
		}
	}
	m.sourceCleanup, m.cleanedCommitIntent = n, data
	if !m.sourceCleanupCurrent(ctx) || !cut("done") || !m.sourceCleanupCurrent(ctx) {
		return deny()
	}
	return p, nil
}

// Read-only exact local intent plus full retained chain under the original
// admission flock. Absence/partial cleanup is uncertainty, not permission to
// create a record. Expired history is readable but cannot renew live authority.
func WithRecoveryCleanedCommitHistory(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryCleanedCommitIntent, RuntimeRecoveryRetentionHistory) error) error {
	if fn == nil {
		return ErrRuntimeMutationBlocked
	}
	return withRecoverySourceCleanupHistory(ctx, dir, operation, b, func(s *recoverySourceCleanupState, h RuntimeRecoveryRetentionHistory) error {
		var p RuntimeRecoveryCleanedCommitIntent
		if s.commitIntent == nil || !exactHistoricalJSON(s.commitIntent.data, &p) || p.Binding != b {
			return ErrRuntimeMutationBlocked
		}
		return fn(p, h)
	})
}
