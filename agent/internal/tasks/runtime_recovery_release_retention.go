package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const runtimeRecoveryReleaseRetentionFile = "recovery-fence-release-retention.json"
const runtimeRecoveryReleaseRetentionLimit = 2 * runtimeResultLimit
const runtimeRecoveryReleaseRetentionPurpose = "runtime_mutation_fence_release_retained_history_v1"

// Private full audit backup, not an unlock record or a claim of deletion.
// Data stays local and is never sent to Manager or included in errors/logs.
type runtimeRecoveryReleaseRetention struct {
	SchemaVersion     int                           `json:"schemaVersion"`
	Purpose           string                        `json:"purpose"`
	OperationID       string                        `json:"operationId"`
	Binding           RuntimeRecoveryGrantBinding   `json:"binding"`
	AuthorizationHash string                        `json:"authorizationHash"`
	PreparedProofHash string                        `json:"preparedProofHash"`
	RetainedAt        time.Time                     `json:"retainedAt"`
	Files             []runtimeRecoveryRetainedFile `json:"files"`
}
type runtimeRecoveryRetainedFile struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Hash  string `json:"hash"`
	Data  []byte `json:"data"`
}
type releaseRetentionSource struct {
	path  string
	limit int64
	info  os.FileInfo
	data  []byte
}

// Caller validates the complete chain and owns the admission flock. Scope/name
// inventory is fixed, bounded and excludes the new backup itself and the lock.
func buildReleaseHistoryRetention(dir, archive string, b RuntimeRecoveryGrantBinding, p RuntimeRecoveryPreparedReleaseProof, retained time.Time) ([]byte, func() bool, error) {
	deny := func() ([]byte, func() bool, error) { return nil, nil, ErrRuntimeMutationBlocked }
	root, e := os.Lstat(dir)
	if e != nil {
		return deny()
	}
	arch, e := os.Lstat(archive)
	if e != nil || privateRetirementArchive(archive) != nil {
		return deny()
	}
	proofWire, e := json.Marshal(p)
	if e != nil {
		return deny()
	}
	r := runtimeRecoveryReleaseRetention{SchemaVersion: 1, Purpose: runtimeRecoveryReleaseRetentionPurpose, OperationID: p.OperationID, Binding: b, AuthorizationHash: p.AuthorizationHash, PreparedProofHash: markerDigest(proofWire), RetainedAt: retained}
	var sources []releaseRetentionSource
	add := func(scope, base, name string, limit int64) error {
		if limit <= 0 {
			return ErrRuntimeMutationBlocked
		}
		path := filepath.Join(base, name)
		info, e := os.Lstat(path)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		data, e := readTerminalConfirmationFile(path, limit)
		after, afterErr := os.Lstat(path)
		if e != nil || afterErr != nil || !os.SameFile(info, after) {
			return ErrRuntimeMutationBlocked
		}
		sources = append(sources, releaseRetentionSource{path, limit, info, data})
		r.Files = append(r.Files, runtimeRecoveryRetainedFile{scope, name, markerDigest(data), data})
		return nil
	}
	names := []string{runtimeRecoveryRetirementFile, runtimeRecoveryTerminalFile, runtimeRecoveryTerminalSubmissionFile, runtimeRecoveryTerminalAcknowledgementFile, "mutation-inflight.json", runtimeRecoveryReleaseIntentFile, runtimeRecoveryPreparedSubmissionFile, runtimeRecoveryPreparedAcknowledgementFile}
	limits := []int64{8192, 8192, 8192, 4096, 16384, 8192, 8192, 4096}
	for i, name := range names {
		if add("root", dir, name, limits[i]) != nil {
			return deny()
		}
	}
	entries, e := os.ReadDir(archive)
	if e != nil || len(entries) > 9 {
		return deny()
	}
	for _, entry := range entries {
		if add("archive", archive, entry.Name(), retirementSourceLimit(entry.Name())) != nil {
			return deny()
		}
	}
	unchanged := func() bool {
		current, e := os.Lstat(dir)
		currentArch, e2 := os.Lstat(archive)
		if e != nil || e2 != nil || !os.SameFile(root, current) || !os.SameFile(arch, currentArch) || privateRetirementArchive(archive) != nil {
			return false
		}
		for _, s := range sources {
			info, e := os.Lstat(s.path)
			data, e2 := readTerminalConfirmationFile(s.path, s.limit)
			after, e3 := os.Lstat(s.path)
			if e != nil || e2 != nil || e3 != nil || !os.SameFile(s.info, info) || !os.SameFile(s.info, after) || !bytes.Equal(s.data, data) {
				return false
			}
		}
		return true
	}
	data, e := json.Marshal(r)
	if e != nil || len(data) > runtimeRecoveryReleaseRetentionLimit || !unchanged() {
		return deny()
	}
	return data, unchanged, nil
}

func validateReleaseHistoryRetention(data []byte, dir, archive string, b RuntimeRecoveryGrantBinding, p RuntimeRecoveryPreparedReleaseProof, expiry time.Time) (func() bool, error) {
	var r runtimeRecoveryReleaseRetention
	if len(data) > runtimeRecoveryReleaseRetentionLimit || !exactHistoricalJSON(data, &r) || r.RetainedAt.IsZero() || r.RetainedAt.Before(p.ObservedAt) || !r.RetainedAt.Before(expiry) {
		return nil, ErrRuntimeMutationBlocked
	}
	expected, unchanged, e := buildReleaseHistoryRetention(dir, archive, b, p, r.RetainedAt)
	if e != nil || !bytes.Equal(data, expected) {
		return nil, ErrRuntimeMutationBlocked
	}
	return unchanged, nil
}

// RetainHistory copies exact validated barriers and archive into one immutable
// private file. It never deletes, resumes or releases anything. The client must
// revalidate current Manager authority in this same callback before calling it.
func (g *RuntimeRecoveryPreparedReleaseGuard) RetainHistory(ctx context.Context) (string, error) {
	return g.retainHistory(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) retainHistory(ctx context.Context, checkpoint func() error) (string, error) {
	if g == nil || g.state == nil {
		return "", ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.preparedSubmission == nil || m.preparedAcknowledgement == nil {
		return "", ErrRuntimeMutationBlocked
	}
	deny := func() (string, error) { m.poisoned = true; return "", ErrRuntimeMutationBlocked }
	p, e := m.preparedReleaseProofLocked(ctx)
	if e != nil {
		return "", ErrRuntimeMutationBlocked
	}
	if m.releaseHistoryRetention != nil {
		return markerDigest(m.releaseHistoryRetention), nil
	}
	data, unchanged, e := buildReleaseHistoryRetention(m.dir, m.archive, m.binding, p, m.lastChecked)
	if e != nil {
		return deny()
	}
	if _, e = m.preparedReleaseProofLocked(ctx); e != nil || !unchanged() || !m.terminalAuthorityCurrent(ctx) {
		return deny()
	}
	if writeRemovalFileNoReplace(filepath.Join(m.dir, runtimeRecoveryReleaseRetentionFile), data) != nil {
		return deny()
	}
	// File and parent are fsynced; a crash before adoption retains every fence.
	if checkpoint != nil && checkpoint() != nil {
		return deny()
	}
	m.releaseHistoryRetention = data
	m.retentionUnchanged = unchanged
	if !unchanged() || m.pinPreparedInventory() != nil {
		return deny()
	}
	if _, e = m.preparedReleaseProofLocked(ctx); e != nil {
		return deny()
	}
	return markerDigest(data), nil
}
