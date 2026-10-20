package tasks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

type RuntimeRecoveryAdmissionOutcome string

const (
	// No final unlink was attempted. This is NOT an assertion about arbitrary
	// external filesystem changes, and partial cleanup still requires recovery.
	RuntimeRecoveryAdmissionNotCommitted RuntimeRecoveryAdmissionOutcome = "not_committed"
	// The final unlink may have happened. Never translate this into "fence
	// retained", and never retry/resume mutation from this result or an archive.
	RuntimeRecoveryAdmissionUncertain RuntimeRecoveryAdmissionOutcome = "uncertain"
	RuntimeRecoveryAdmissionDurable   RuntimeRecoveryAdmissionOutcome = "durable"
)

// Local outcome only, never a Manager completion receipt or revocation proof.
// Callers MUST retain it even when the enclosing readiness callback fails.
type RuntimeRecoveryAdmissionReleaseResult struct {
	OperationID, ArchiveHash string
	RemovedFiles             int
	Outcome                  RuntimeRecoveryAdmissionOutcome
}

type runtimeRecoveryAdmissionRelease struct {
	files          [5]releaseRetentionSource
	root, lockInfo os.FileInfo
	removed        int
	result         RuntimeRecoveryAdmissionReleaseResult
}

// Validate the exact deletion prefix against ORIGINAL inode snapshots. Never
// adopt a new root, lock, archive, same-byte record or an unexpected file.
func (m *RuntimeRecoveryFinalization) admissionReleaseFilesCurrent() bool {
	s := m.admissionRelease
	if s == nil || s.removed < 0 || s.removed > len(s.files) || m.lock == nil {
		return false
	}
	root, e := privateRecoveryHistoryDir(m.dir, false)
	lock, e2 := m.lock.Stat()
	pathLock, e3 := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if e != nil || e2 != nil || e3 != nil || !os.SameFile(s.root, root) || !os.SameFile(s.lockInfo, lock) || !os.SameFile(lock, pathLock) {
		return false
	}
	stat, ok := lock.Sys().(*syscall.Stat_t)
	if !ok || !lock.Mode().IsRegular() || lock.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return false
	}
	names := map[string]bool{"runtime.lock": true}
	for i, f := range s.files {
		if i < s.removed {
			if !runtimeMarkerAbsent(f.path) {
				return false
			}
			continue
		}
		before, e := os.Lstat(f.path)
		data, e2 := readTerminalConfirmationFile(f.path, f.limit)
		after, e3 := os.Lstat(f.path)
		if e != nil || e2 != nil || e3 != nil || !os.SameFile(f.info, before) || !os.SameFile(f.info, after) || !bytes.Equal(data, f.data) {
			return false
		}
		names[filepath.Base(f.path)] = true
	}
	entries, e := os.ReadDir(m.dir)
	if e != nil || len(entries) != len(names) {
		return false
	}
	for _, entry := range entries {
		if !names[entry.Name()] {
			return false
		}
	}
	return true
}

func (m *RuntimeRecoveryFinalization) admissionReleaseCurrent(ctx context.Context) bool {
	check := func() bool {
		return m.self == m && m.terminalAuthorityCurrent(ctx) && m.cleanedCommitArchive != nil &&
			m.cleanedArchiveUnchanged != nil && m.cleanedArchiveUnchanged() &&
			m.historyArchiveUnchanged != nil && m.historyArchiveUnchanged() && m.admissionReleaseFilesCurrent()
	}
	if !check() {
		return false
	}
	started := m.now().UTC()
	bounded, cancel := context.WithDeadline(ctx, m.authorization.ExpiresAt)
	defer cancel()
	path, e := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
	if e != nil {
		return false
	}
	o, e := observeRuntimeRecoveryLocked(bounded, filepath.Join(path, "history", "retired-"+m.authorization.OperationID), m.binding.RuntimeResultBinding, m.authorization.Expected.TaskID, m.activePath, m.adapter, m.lock)
	finished := m.now().UTC()
	if e != nil || bounded.Err() != nil || finished.Before(started) || !finished.Before(m.authorization.ExpiresAt) || o != m.authorization.Expected || !check() || !m.terminalAuthorityCurrent(ctx) {
		return false
	}
	m.lastChecked = finished
	return true
}

// One-shot original-owner release, called only inside fresh authenticated
// cleaned readiness. Both independent archives MUST already be owned/durable.
// Remove four auxiliary blockers first and the legacy marker LAST, retaining
// the ORIGINAL flock even after success. Any prefix before that last unlink
// remains fenced. No resume, automatic repair, runtime apply or archive removal.
func (g *RuntimeRecoveryPreparedReleaseGuard) ReleaseAdmission(ctx context.Context) (RuntimeRecoveryAdmissionReleaseResult, error) {
	return g.releaseAdmission(ctx, nil)
}

func (g *RuntimeRecoveryPreparedReleaseGuard) releaseAdmission(ctx context.Context, checkpoint func(string) error) (RuntimeRecoveryAdmissionReleaseResult, error) {
	zero := RuntimeRecoveryAdmissionReleaseResult{Outcome: RuntimeRecoveryAdmissionNotCommitted}
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.self != m || m.admissionRelease != nil {
		return zero, ErrRuntimeMutationBlocked
	}
	plan, e := m.planCleanedCommitLocked(ctx)
	if e != nil || m.cleanedCommitArchive == nil || m.cleanedArchiveUnchanged == nil || !m.cleanedArchiveUnchanged() {
		return zero, ErrRuntimeMutationBlocked
	}
	files, e := cleanedCommitArchiveSources(m.sourceCleanup)
	if e != nil {
		return zero, ErrRuntimeMutationBlocked
	}
	for i, f := range files {
		if plan.Files[i].Name != filepath.Base(f.path) || plan.Files[i].Hash != markerDigest(f.data) {
			m.poisoned = true
			return zero, ErrRuntimeMutationBlocked
		}
	}
	s := &runtimeRecoveryAdmissionRelease{root: m.sourceCleanup.root, lockInfo: m.sourceCleanup.lockInfo,
		files:  [5]releaseRetentionSource{files[4], files[3], files[2], files[1], files[0]},
		result: RuntimeRecoveryAdmissionReleaseResult{OperationID: plan.OperationID, ArchiveHash: markerDigest(m.cleanedCommitArchive), Outcome: RuntimeRecoveryAdmissionNotCommitted}}
	m.admissionRelease = s
	deny := func() (RuntimeRecoveryAdmissionReleaseResult, error) {
		m.poisoned = true
		return s.result, ErrRuntimeMutationBlocked
	}
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	if !cut("before") || !m.admissionReleaseCurrent(ctx) {
		return deny()
	}
	for i, f := range s.files {
		if !m.admissionReleaseCurrent(ctx) {
			return deny()
		}
		if i == len(s.files)-1 && (!cut("before-final") || !m.admissionReleaseCurrent(ctx)) {
			return deny()
		}
		e := unlinkRecoverySource(f, func(step string) error {
			if step == "unlinked" {
				// Track the actual prefix BEFORE fsync. An injected/I/O failure
				// here must not be reported as a retained final marker.
				s.removed = i + 1
				s.result.RemovedFiles = s.removed
			}
			if !cut(step+"-"+strconv.Itoa(i+1)) || !m.admissionReleaseCurrent(ctx) {
				return ErrRuntimeMutationBlocked
			}
			if step == "pinned" && i == len(s.files)-1 {
				// Set conservatively immediately BEFORE the final os.Remove.
				s.result.Outcome = RuntimeRecoveryAdmissionUncertain
			}
			return nil
		})
		if e != nil || !cut("file-"+strconv.Itoa(i+1)) || !m.admissionReleaseCurrent(ctx) {
			return deny()
		}
	}
	if !cut("done") || !m.admissionReleaseCurrent(ctx) {
		return deny()
	}
	s.result.Outcome = RuntimeRecoveryAdmissionDurable
	return s.result, nil
}
