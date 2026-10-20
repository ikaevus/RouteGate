package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Local filesystem observation only. Neither this value nor historical readback
// grants deletion, runtime mutation, fence release or reservation completion.
type RuntimeRecoverySourceCleanupStatus struct {
	OperationID, ManifestHash string
	RemovedFiles, TotalFiles  int
	SourcesRemoved            bool
}

type recoverySourceCleanupState struct {
	dir, operation, hash            string
	lock                            *os.File
	files                           []recoveryHistoryCopyFile
	root, archive, lockInfo         os.FileInfo
	sources, barriers               []releaseRetentionSource
	removed                         int
	archiveRemoved                  bool
	commitIntent                    *releaseRetentionSource
	commitSubmission, commitReceipt *releaseRetentionSource
}

// Only validated fixed paths from the complete independent archive enter here.
// Missing originals must form a prefix of the fixed source order, never holes.
func readRecoverySourceCleanup(dir, operation string, lock *os.File, files []recoveryHistoryCopyFile, hash string) (*recoverySourceCleanupState, error) {
	deny := func() (*recoverySourceCleanupState, error) { return nil, ErrRuntimeMutationBlocked }
	root, err := privateRecoveryHistoryDir(dir, false)
	if err != nil || lock == nil {
		return deny()
	}
	info, err := os.Lstat(filepath.Join(dir, "runtime.lock"))
	held, e := lock.Stat()
	if err != nil || e != nil || !os.SameFile(info, held) {
		return deny()
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return deny()
	}
	s := &recoverySourceCleanupState{dir: dir, operation: operation, hash: hash, lock: lock, files: files, root: root, lockInfo: info}
	archive, err := retirementArchivePath(dir, operation)
	if err != nil {
		return deny()
	}
	if runtimeMarkerAbsent(archive) {
		s.archiveRemoved = true
	} else {
		s.archive, err = privateRecoveryHistoryDir(archive, false)
		if err != nil {
			return deny()
		}
	}
	rootNames := map[string]bool{"runtime.lock": true}
	archiveNames := map[string]bool{}
	if !s.archiveRemoved {
		rootNames[filepath.Base(archive)] = true
	}
	remaining := false
	for _, f := range files {
		v := f.metadata
		base := dir
		if v.Scope == "archive" {
			base = archive
		} else if v.Scope != "root" {
			return deny()
		}
		path := filepath.Join(base, v.Name)
		barrier := v.Scope == "root" && (v.Name == "mutation-inflight.json" || v.Name == runtimeRecoveryReleaseIntentFile)
		source := releaseRetentionSource{path: path, limit: int64(len(f.data)), data: f.data}
		if runtimeMarkerAbsent(path) {
			if barrier || remaining {
				return deny()
			}
			s.removed++
		} else {
			before, err := os.Lstat(path)
			data, e := readTerminalConfirmationFile(path, source.limit)
			after, e2 := os.Lstat(path)
			if err != nil || e != nil || e2 != nil || !os.SameFile(before, after) || !bytes.Equal(data, f.data) {
				return deny()
			}
			source.info = before
			if v.Scope == "root" {
				rootNames[v.Name] = true
			} else {
				archiveNames[v.Name] = true
			}
			if !barrier {
				remaining = true
			}
		}
		if barrier {
			s.barriers = append(s.barriers, source)
		} else {
			s.sources = append(s.sources, source)
		}
	}
	if len(s.barriers) != 2 || (s.archiveRemoved && s.removed != len(s.sources)) {
		return deny()
	}
	intentPath := filepath.Join(dir, runtimeRecoveryCleanedCommitIntentFile)
	if !runtimeMarkerAbsent(intentPath) {
		if !s.status().SourcesRemoved {
			return deny()
		}
		before, err := os.Lstat(intentPath)
		data, e := readTerminalConfirmationFile(intentPath, 8192)
		after, e2 := os.Lstat(intentPath)
		var intent RuntimeRecoveryCleanedCommitIntent
		if err != nil || e != nil || e2 != nil || !os.SameFile(before, after) || !exactHistoricalJSON(data, &intent) {
			return deny()
		}
		expected, e := s.cleanedCommitIntent(intent.ObservedAt)
		if e != nil || intent != expected {
			return deny()
		}
		s.commitIntent = &releaseRetentionSource{intentPath, 8192, before, data}
		rootNames[runtimeRecoveryCleanedCommitIntentFile] = true
	}
	for _, name := range []string{runtimeRecoveryCleanedCommitSubmissionFile, runtimeRecoveryCleanedCommitAcknowledgementFile} {
		path := filepath.Join(dir, name)
		if runtimeMarkerAbsent(path) {
			continue
		}
		before, err := os.Lstat(path)
		data, e := readTerminalConfirmationFile(path, 8192)
		after, e2 := os.Lstat(path)
		if err != nil || e != nil || e2 != nil || !os.SameFile(before, after) || s.commitIntent == nil {
			return deny()
		}
		if name == runtimeRecoveryCleanedCommitSubmissionFile {
			var p RuntimeRecoveryCleanedCommitProof
			var intent RuntimeRecoveryCleanedCommitIntent
			if !exactHistoricalJSON(data, &p) || !exactHistoricalJSON(s.commitIntent.data, &intent) || p != cleanedCommitProof(intent, s.commitIntent.data, p.ObservedAt) || p.ObservedAt.Before(intent.ObservedAt) || !p.ObservedAt.Before(intent.ExpiresAt) {
				return deny()
			}
			s.commitSubmission = &releaseRetentionSource{path, 8192, before, data}
		} else {
			var r RuntimeRecoveryCleanedCommitReceipt
			var p RuntimeRecoveryCleanedCommitProof
			if s.commitSubmission == nil || !exactHistoricalJSON(s.commitSubmission.data, &p) || !exactHistoricalJSON(data, &r) || !cleanedCommitReceiptMatches(r, p, s.commitSubmission.data) {
				return deny()
			}
			s.commitReceipt = &releaseRetentionSource{path, 8192, before, data}
		}
		rootNames[name] = true
	}
	exact := func(path string, names map[string]bool) bool {
		entries, e := os.ReadDir(path)
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
	if !exact(dir, rootNames) || !s.archiveRemoved && !exact(archive, archiveNames) {
		return deny()
	}
	return s, nil
}

func sameCleanupInfo(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b)
}
func sameCleanupRecord(a, b *releaseRetentionSource) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a.info, b.info) && bytes.Equal(a.data, b.data)
}
func (s *recoverySourceCleanupState) unchanged() bool {
	n, e := readRecoverySourceCleanup(s.dir, s.operation, s.lock, s.files, s.hash)
	if e != nil || n.removed != s.removed || n.archiveRemoved != s.archiveRemoved || !sameCleanupInfo(s.root, n.root) || !sameCleanupInfo(s.archive, n.archive) || !sameCleanupInfo(s.lockInfo, n.lockInfo) || len(s.sources) != len(n.sources) || len(s.barriers) != len(n.barriers) {
		return false
	}
	if (s.commitIntent == nil) != (n.commitIntent == nil) || s.commitIntent != nil && (!os.SameFile(s.commitIntent.info, n.commitIntent.info) || !bytes.Equal(s.commitIntent.data, n.commitIntent.data)) {
		return false
	}
	if !sameCleanupRecord(s.commitSubmission, n.commitSubmission) || !sameCleanupRecord(s.commitReceipt, n.commitReceipt) {
		return false
	}
	for i, v := range s.sources {
		if !sameCleanupInfo(v.info, n.sources[i].info) {
			return false
		}
	}
	for i, v := range s.barriers {
		if !sameCleanupInfo(v.info, n.barriers[i].info) {
			return false
		}
	}
	return true
}
func (s *recoverySourceCleanupState) status() RuntimeRecoverySourceCleanupStatus {
	return RuntimeRecoverySourceCleanupStatus{s.operation, s.hash, s.removed, len(s.sources), s.archiveRemoved && s.removed == len(s.sources)}
}

// Pin the validated inode across the pathname unlink. Checking only the missing
// path afterward cannot distinguish removal of a same-byte replacement while
// the original was renamed away. The open original must have no links left.
func unlinkRecoverySource(source releaseRetentionSource, checkpoint func(string) error) error {
	fd, err := syscall.Open(source.path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	file := os.NewFile(uintptr(fd), source.path)
	defer file.Close()
	check := func(links uint64) bool {
		private := func(info os.FileInfo) bool {
			stat, ok := info.Sys().(*syscall.Stat_t)
			return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && stat.Uid == uint32(os.Geteuid()) && uint64(stat.Nlink) == links && os.SameFile(source.info, info)
		}
		before, e := file.Stat()
		if e != nil || !private(before) {
			return false
		}
		data, e := io.ReadAll(io.NewSectionReader(file, 0, int64(len(source.data))+1))
		after, e2 := file.Stat()
		return e == nil && e2 == nil && private(after) && bytes.Equal(data, source.data)
	}
	if !check(1) {
		return ErrRuntimeMutationBlocked
	}
	if checkpoint != nil && checkpoint("pinned") != nil {
		return ErrRuntimeMutationBlocked
	}
	return removeRemovalFileCheckpoint(source.path, func() error {
		if !check(0) {
			return ErrRuntimeMutationBlocked
		}
		if checkpoint != nil {
			return checkpoint("unlinked")
		}
		return nil
	})
}

func (m *RuntimeRecoveryFinalization) sourceCleanupCurrent(ctx context.Context) bool {
	if m.admissionRelease != nil {
		return false
	}
	s := m.sourceCleanup
	if m.cleanedCommitArchive != nil && (m.cleanedArchiveUnchanged == nil || !m.cleanedArchiveUnchanged()) {
		return false
	}
	if s != nil && (!ownedCleanupRecord(s.commitSubmission, m.cleanedCommitSubmission) || !ownedCleanupRecord(s.commitReceipt, m.cleanedCommitReceipt)) {
		return false
	}
	if m.self != m || s == nil || (s.commitIntent == nil) != (m.cleanedCommitIntent == nil) || s.commitIntent != nil && !bytes.Equal(s.commitIntent.data, m.cleanedCommitIntent) || !m.terminalAuthorityCurrent(ctx) || m.historyArchiveUnchanged == nil || !m.historyArchiveUnchanged() || !s.unchanged() {
		return false
	}
	started := m.now().UTC()
	bounded, cancel := context.WithDeadline(ctx, m.authorization.ExpiresAt)
	defer cancel()
	copyPath, e := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
	if e != nil {
		return false
	}
	o, e := observeRuntimeRecoveryLocked(bounded, filepath.Join(copyPath, "history", "retired-"+s.operation), m.binding.RuntimeResultBinding, m.authorization.Expected.TaskID, m.activePath, m.adapter, m.lock)
	finished := m.now().UTC()
	if e != nil || bounded.Err() != nil || finished.Before(started) || !finished.Before(m.authorization.ExpiresAt) || o != m.authorization.Expected || !m.historyArchiveUnchanged() || !s.unchanged() {
		return false
	}
	m.lastChecked = finished
	return true
}

// The same original owner may retry a completed cleanup. Partial/uncertain
// cleanup poisons it; historical bytes never create a replacement live owner.
func (m *RuntimeRecoveryFinalization) WithSourceCleanupSubmission(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string, fn func(*RuntimeRecoveryPreparedReleaseGuard) error) error {
	if m == nil || m.self != m || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binding != b || m.authorizationHash != authorizationHash {
		return ErrRuntimeMutationBlocked
	}
	// One-shot transition: neither a partial deletion nor completed admission
	// release can be entered again through historical/source readiness.
	if m.admissionRelease != nil {
		return ErrRuntimeMutationBlocked
	}
	check := func() bool {
		if m.sourceCleanup != nil {
			return m.sourceCleanup.status().SourcesRemoved && m.sourceCleanupCurrent(ctx)
		}
		_, e := m.retentionProofLocked(ctx)
		return e == nil && m.preparedAcknowledgement != nil && m.retentionReceipt != nil && m.historyArchive != nil
	}
	if !check() {
		return ErrRuntimeMutationBlocked
	}
	g := &RuntimeRecoveryPreparedReleaseGuard{state: &runtimeRecoveryPreparedReleaseGuardState{owner: m}}
	defer func() { g.state.mu.Lock(); g.state.owner = nil; g.state.mu.Unlock() }()
	err := fn(g)
	g.state.mu.Lock()
	current := false
	if m.admissionRelease != nil {
		current = m.admissionRelease.result.Outcome == RuntimeRecoveryAdmissionDurable && m.admissionReleaseCurrent(ctx)
	} else {
		current = check()
	}
	g.state.mu.Unlock()
	if !current {
		m.poisoned = true
		return ErrRuntimeMutationBlocked
	}
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	return nil
}

func (g *RuntimeRecoveryPreparedReleaseGuard) CleanupPreparedProof(ctx context.Context) (RuntimeRecoveryPreparedReleaseProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.self != m || m.preparedAcknowledgement == nil {
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	if m.sourceCleanup == nil {
		return m.preparedReleaseProofLocked(ctx)
	}
	if !m.sourceCleanup.status().SourcesRemoved || !m.sourceCleanupCurrent(ctx) {
		m.poisoned = true
		return RuntimeRecoveryPreparedReleaseProof{}, ErrRuntimeMutationBlocked
	}
	return m.preparedFrozen, nil
}
func (g *RuntimeRecoveryPreparedReleaseGuard) CleanupRetentionProof(ctx context.Context) (RuntimeRecoveryRetentionProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryRetentionProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.self != m || m.retentionReceipt == nil {
		return RuntimeRecoveryRetentionProof{}, ErrRuntimeMutationBlocked
	}
	if m.sourceCleanup == nil {
		return m.retentionProofLocked(ctx)
	}
	if !m.sourceCleanup.status().SourcesRemoved || !m.sourceCleanupCurrent(ctx) {
		m.poisoned = true
		return RuntimeRecoveryRetentionProof{}, ErrRuntimeMutationBlocked
	}
	return m.retentionFrozen, nil
}

func (g *RuntimeRecoveryPreparedReleaseGuard) CleanupArchivedSources(ctx context.Context) (RuntimeRecoverySourceCleanupStatus, error) {
	return g.cleanupArchivedSources(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) cleanupArchivedSources(ctx context.Context, checkpoint func(string) error) (RuntimeRecoverySourceCleanupStatus, error) {
	var zero RuntimeRecoverySourceCleanupStatus
	if g == nil || g.state == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.self != m {
		return zero, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoverySourceCleanupStatus, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	if m.sourceCleanup != nil {
		if !m.sourceCleanup.status().SourcesRemoved || !m.sourceCleanupCurrent(ctx) {
			return deny()
		}
		return m.sourceCleanup.status(), nil
	}
	plan, e := m.planArchivedSourceCleanupLocked(ctx, nil)
	if e != nil {
		return zero, ErrRuntimeMutationBlocked
	}
	files, stable, e := collectRecoveryHistory(m.dir, plan.OperationID)
	if e != nil || !stable() {
		return deny()
	}
	s, e := readRecoverySourceCleanup(m.dir, plan.OperationID, m.lock, files, plan.ManifestHash)
	if e != nil || s.removed != 0 || s.archiveRemoved {
		return deny()
	}
	m.sourceCleanup = s
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	if !m.sourceCleanupCurrent(ctx) || !cut("before") {
		return deny()
	}
	for i := range s.sources {
		if !m.sourceCleanupCurrent(ctx) {
			return deny()
		}
		v := &s.sources[i]
		if unlinkRecoverySource(*v, func(step string) error {
			if !cut(step + "-" + strconv.Itoa(i+1)) {
				return ErrRuntimeMutationBlocked
			}
			return nil
		}) != nil {
			return deny()
		}
		v.info = nil
		s.removed = i + 1
		if !cut("file-"+strconv.Itoa(i+1)) || !m.sourceCleanupCurrent(ctx) {
			return deny()
		}
	}
	archive := filepath.Join(m.dir, plan.OriginalArchiveName)
	if !m.sourceCleanupCurrent(ctx) || os.Remove(archive) != nil || !cut("directory-unlinked") || syncRetirementDir(m.dir) != nil {
		return deny()
	}
	s.archive = nil
	s.archiveRemoved = true
	if !cut("directory") || !m.sourceCleanupCurrent(ctx) {
		return deny()
	}
	return s.status(), nil
}

// Read-only classification under the ORIGINAL admission lock. Partial prefix
// states remain fenced and cannot resume. No runtime or authority is inferred.
func WithRecoverySourceCleanupHistory(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoverySourceCleanupStatus, RuntimeRecoveryRetentionHistory) error) error {
	if fn == nil {
		return ErrRuntimeMutationBlocked
	}
	return withRecoverySourceCleanupHistory(ctx, dir, operation, b, func(s *recoverySourceCleanupState, h RuntimeRecoveryRetentionHistory) error {
		return fn(s.status(), h)
	})
}

func withRecoverySourceCleanupHistory(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(*recoverySourceCleanupState, RuntimeRecoveryRetentionHistory) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	path, e := recoveryHistoryArchivePath(dir, operation)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	parent, e := privateRecoveryHistoryDir(filepath.Dir(dir), true)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	parentCurrent := func() bool {
		p, e := privateRecoveryHistoryDir(filepath.Dir(dir), true)
		return e == nil && os.SameFile(parent, p)
	}
	lock, e := lockRuntimeMutationExisting(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	return withRecoveryHistoryArchive(ctx, path, b, operation, func(h RuntimeRecoveryRetentionHistory, hash string, stable func() bool) error {
		files, _, e := collectRecoveryHistory(filepath.Join(path, "history"), operation)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		s, e := readRecoverySourceCleanup(dir, operation, lock, files, hash)
		if e != nil || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		if !parentCurrent() || fn(s, h) != nil || !parentCurrent() || !s.unchanged() || !stable() || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}
