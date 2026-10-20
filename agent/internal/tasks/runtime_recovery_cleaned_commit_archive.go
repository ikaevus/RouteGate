package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const cleanedCommitArchivePurpose = "runtime_mutation_cleaned_commit_history_v1"

type cleanedCommitArchiveManifest struct {
	SchemaVersion           int                           `json:"schemaVersion"`
	Purpose                 string                        `json:"purpose"`
	OperationID             string                        `json:"operationId"`
	Binding                 RuntimeRecoveryGrantBinding   `json:"binding"`
	PredecessorManifestHash string                        `json:"predecessorManifestHash"`
	CleanedIntentHash       string                        `json:"cleanedIntentHash"`
	ProofHash               string                        `json:"proofHash"`
	ReceiptHash             string                        `json:"receiptHash"`
	Files                   [5]recoveryHistoryArchiveFile `json:"files"`
}

type RuntimeRecoveryCleanedCommitArchivedHistory struct {
	Intent       RuntimeRecoveryCleanedCommitIntent
	Proof        RuntimeRecoveryCleanedCommitProof
	Receipt      RuntimeRecoveryCleanedCommitReceipt
	ManifestHash string
}

func cleanedCommitArchivePath(dir, operation string) (string, error) {
	if _, e := recoveryHistoryArchivePath(dir, operation); e != nil {
		return "", e
	}
	return filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-cleaned-history-"+operation), nil
}

func cleanedCommitArchiveSources(s *recoverySourceCleanupState) ([5]releaseRetentionSource, error) {
	var result [5]releaseRetentionSource
	if s == nil || !s.status().SourcesRemoved || s.commitIntent == nil || s.commitSubmission == nil || s.commitReceipt == nil {
		return result, ErrRuntimeMutationBlocked
	}
	for _, f := range s.barriers {
		switch filepath.Base(f.path) {
		case "mutation-inflight.json":
			result[0] = f
		case runtimeRecoveryReleaseIntentFile:
			result[1] = f
		}
	}
	result[2], result[3], result[4] = *s.commitIntent, *s.commitSubmission, *s.commitReceipt
	for _, f := range result {
		if f.info == nil || len(f.data) == 0 {
			return [5]releaseRetentionSource{}, ErrRuntimeMutationBlocked
		}
	}
	return result, nil
}

func cleanedCommitArchiveRecord(b RuntimeRecoveryGrantBinding, previous string, s *recoverySourceCleanupState) (cleanedCommitArchiveManifest, error) {
	var zero cleanedCommitArchiveManifest
	files, e := cleanedCommitArchiveSources(s)
	if e != nil {
		return zero, e
	}
	r := cleanedCommitArchiveManifest{SchemaVersion: 1, Purpose: cleanedCommitArchivePurpose, OperationID: s.operation, Binding: b,
		PredecessorManifestHash: previous, CleanedIntentHash: markerDigest(files[2].data), ProofHash: markerDigest(files[3].data), ReceiptHash: markerDigest(files[4].data)}
	for i, f := range files {
		r.Files[i] = recoveryHistoryArchiveFile{Scope: "root", Name: filepath.Base(f.path), Hash: markerDigest(f.data)}
	}
	return r, nil
}

// The ORIGINAL admission flock is held by the caller. This private copy has a
// distinct read-lock stub; its data can never reconstruct a live runtime owner.
func withCleanedCommitArchive(ctx context.Context, path, operation string, b RuntimeRecoveryGrantBinding, files []recoveryHistoryCopyFile, previous string, previousCurrent func() bool, fn func(RuntimeRecoveryCleanedCommitArchivedHistory, func() bool) error) error {
	deny := func() error { return ErrRuntimeMutationBlocked }
	if ctx.Err() != nil || fn == nil || previousCurrent == nil || !previousCurrent() {
		return deny()
	}
	outer, e := privateRecoveryHistoryDir(path, false)
	if e != nil {
		return deny()
	}
	parent, e := privateRecoveryHistoryDir(filepath.Dir(path), true)
	if e != nil {
		return deny()
	}
	outerCurrent := func() bool {
		o, e := privateRecoveryHistoryDir(path, false)
		p, e2 := privateRecoveryHistoryDir(filepath.Dir(path), true)
		entries, e3 := os.ReadDir(path)
		return e == nil && e2 == nil && e3 == nil && os.SameFile(outer, o) && os.SameFile(parent, p) && len(entries) == 2 && entries[0].Name() == recoveryHistoryArchiveManifest && entries[1].Name() == "state"
	}
	manifestPath := filepath.Join(path, recoveryHistoryArchiveManifest)
	manifestInfo, e := os.Lstat(manifestPath)
	if e != nil || !outerCurrent() {
		return deny()
	}
	data, e := readTerminalConfirmationFile(manifestPath, 16384)
	var record cleanedCommitArchiveManifest
	if e != nil || !exactHistoricalJSON(data, &record) || record.OperationID != operation || record.Binding != b || record.PredecessorManifestHash != previous {
		return deny()
	}
	state := filepath.Join(path, "state")
	lock, e := lockRuntimeMutationExisting(state)
	if e != nil {
		return deny()
	}
	defer lock.Close()
	stubPath := filepath.Join(state, "runtime.lock")
	stub, e := readTerminalConfirmationFile(stubPath, int64(len(recoveryHistoryArchiveLock)))
	if e != nil || string(stub) != recoveryHistoryArchiveLock {
		return deny()
	}
	s, e := readRecoverySourceCleanup(state, operation, lock, files, previous)
	if e != nil {
		return deny()
	}
	expected, e := cleanedCommitArchiveRecord(b, previous, s)
	if e != nil {
		return deny()
	}
	wire, e := json.Marshal(expected)
	if e != nil || !bytes.Equal(wire, data) {
		return deny()
	}
	var h RuntimeRecoveryCleanedCommitArchivedHistory
	if !exactHistoricalJSON(s.commitIntent.data, &h.Intent) || h.Intent.Binding != b || !exactHistoricalJSON(s.commitSubmission.data, &h.Proof) || !exactHistoricalJSON(s.commitReceipt.data, &h.Receipt) {
		return deny()
	}
	h.ManifestHash = markerDigest(data)
	stable := func() bool {
		info, e := os.Lstat(manifestPath)
		dataNow, e2 := readTerminalConfirmationFile(manifestPath, 16384)
		after, e3 := os.Lstat(manifestPath)
		stub, e4 := readTerminalConfirmationFile(stubPath, int64(len(recoveryHistoryArchiveLock)))
		if e != nil || e2 != nil || e3 != nil || e4 != nil || !os.SameFile(manifestInfo, info) || !os.SameFile(manifestInfo, after) || !bytes.Equal(data, dataNow) || string(stub) != recoveryHistoryArchiveLock || !outerCurrent() || !previousCurrent() {
			return false
		}
		// Stable checks outlive this reader's lock descriptor in a live owner.
		// Reopen read-only, then compare with the ORIGINAL captured stub inode.
		fd, e := syscall.Open(stubPath, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if e != nil {
			return false
		}
		f := os.NewFile(uintptr(fd), stubPath)
		defer f.Close()
		copy := *s
		copy.lock = f
		return copy.unchanged()
	}
	if !stable() || ctx.Err() != nil || fn(h, stable) != nil || ctx.Err() != nil || !stable() {
		return deny()
	}
	return nil
}

// Copy only from the original live owner after authenticated Manager readiness.
// The predecessor archive and all root barriers/receipts remain untouched.
func (g *RuntimeRecoveryPreparedReleaseGuard) ArchiveCleanedCommitHistory(ctx context.Context) (string, error) {
	return g.archiveCleanedCommitHistory(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) archiveCleanedCommitHistory(ctx context.Context, checkpoint func(string) error) (string, error) {
	if g == nil || g.state == nil {
		return "", ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil {
		return "", ErrRuntimeMutationBlocked
	}
	deny := func() (string, error) { m.poisoned = true; return "", ErrRuntimeMutationBlocked }
	plan, e := m.planCleanedCommitLocked(ctx)
	if e != nil {
		return "", e
	}
	if m.cleanedCommitArchive != nil {
		return markerDigest(m.cleanedCommitArchive), nil
	}
	path, e := cleanedCommitArchivePath(m.dir, plan.OperationID)
	if e != nil {
		return deny()
	}
	sources, e := cleanedCommitArchiveSources(m.sourceCleanup)
	if e != nil {
		return deny()
	}
	for i, f := range sources {
		if plan.Files[i].Name != filepath.Base(f.path) || plan.Files[i].Hash != markerDigest(f.data) {
			return deny()
		}
	}
	record, e := cleanedCommitArchiveRecord(m.binding, plan.ManifestHash, m.sourceCleanup)
	if e != nil {
		return deny()
	}
	manifest, e := json.Marshal(record)
	if e != nil || len(manifest) > 16384 {
		return deny()
	}
	parent, e := privateRecoveryHistoryDir(filepath.Dir(path), true)
	if e != nil {
		return deny()
	}
	parentCurrent := func() bool {
		p, e := privateRecoveryHistoryDir(filepath.Dir(path), true)
		return e == nil && os.SameFile(parent, p)
	}
	current := func() bool { return parentCurrent() && m.sourceCleanupCurrent(ctx) && m.terminalAuthorityCurrent(ctx) }
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	if !current() || !cut("before") || !current() || os.Mkdir(path, 0700) != nil {
		return deny()
	}
	outer, e := privateRecoveryHistoryDir(path, false)
	if e != nil {
		return deny()
	}
	if syncRetirementDir(filepath.Dir(path)) != nil || !cut("directory") {
		return deny()
	}
	state := filepath.Join(path, "state")
	outerNow, outerErr := privateRecoveryHistoryDir(path, false)
	if outerErr != nil || !os.SameFile(outer, outerNow) || !parentCurrent() || os.Mkdir(state, 0700) != nil {
		return deny()
	}
	stateInfo, e := privateRecoveryHistoryDir(state, false)
	if e != nil {
		return deny()
	}
	directoriesCurrent := func() bool {
		o, e := privateRecoveryHistoryDir(path, false)
		s, e2 := privateRecoveryHistoryDir(state, false)
		return e == nil && e2 == nil && parentCurrent() && os.SameFile(outer, o) && os.SameFile(stateInfo, s)
	}
	if syncRetirementDir(path) != nil || !cut("state") || !directoriesCurrent() {
		return deny()
	}
	type pinnedCopy struct {
		path string
		file *os.File
		data []byte
	}
	var copied []pinnedCopy
	defer func() {
		for _, f := range copied {
			f.file.Close()
		}
	}()
	write := func(path string, data []byte) bool {
		var pinned *os.File
		err := publishRemovalFileCheckpoint(path, data, false, func(step string, file *os.File) error {
			if step == "file" {
				fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
				if errno != 0 {
					return ErrRuntimeMutationBlocked
				}
				pinned = os.NewFile(fd, path)
			}
			return nil
		})
		if pinned != nil {
			copied = append(copied, pinnedCopy{path, pinned, data})
		}
		return err == nil && pinned != nil
	}
	filesCurrent := func() bool {
		for _, f := range copied {
			held, e := f.file.Stat()
			info, e2 := os.Lstat(f.path)
			data, e3 := readTerminalConfirmationFile(f.path, int64(len(f.data)))
			after, e4 := os.Lstat(f.path)
			if e != nil || e2 != nil || e3 != nil || e4 != nil || !os.SameFile(held, info) || !os.SameFile(held, after) || !bytes.Equal(f.data, data) {
				return false
			}
		}
		return true
	}
	if !write(filepath.Join(state, "runtime.lock"), []byte(recoveryHistoryArchiveLock)) || !cut("lock") {
		return deny()
	}
	for i, f := range sources {
		if !current() || !directoriesCurrent() || !filesCurrent() || !write(filepath.Join(state, filepath.Base(f.path)), f.data) || !cut("copy-"+strconv.Itoa(i)) {
			return deny()
		}
	}
	if !cut("copied") || !current() || !directoriesCurrent() || !filesCurrent() || !write(filepath.Join(path, recoveryHistoryArchiveManifest), manifest) || !cut("published") {
		return deny()
	}
	var unchanged func() bool
	if withCleanedCommitArchive(ctx, path, plan.OperationID, m.binding, m.sourceCleanup.files, plan.ManifestHash, m.historyArchiveUnchanged, func(h RuntimeRecoveryCleanedCommitArchivedHistory, stable func() bool) error {
		if h.ManifestHash != markerDigest(manifest) || h.Proof != m.cleanedCommitFrozen {
			return ErrRuntimeMutationBlocked
		}
		unchanged = stable
		return nil
	}) != nil || !current() || !directoriesCurrent() || !filesCurrent() || unchanged == nil || !unchanged() {
		return deny()
	}
	m.cleanedCommitArchive = manifest
	m.cleanedArchiveUnchanged = func() bool { return parentCurrent() && unchanged() }
	if !cut("done") || !current() {
		return deny()
	}
	return markerDigest(manifest), nil
}

// Full chained history under the ORIGINAL admission lock. No runtime capture,
// file repair, authority renewal or owner reconstruction, including after expiry.
func WithArchivedRecoveryCleanedCommit(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryCleanedCommitArchivedHistory) error) error {
	if fn == nil {
		return ErrRuntimeMutationBlocked
	}
	path, e := cleanedCommitArchivePath(dir, operation)
	if e != nil {
		return e
	}
	previous, e := recoveryHistoryArchivePath(dir, operation)
	if e != nil {
		return e
	}
	return WithArchivedRecoveryRetention(ctx, dir, operation, b, func(h RuntimeRecoveryArchivedHistory) error {
		files, current, e := collectRecoveryHistory(filepath.Join(previous, "history"), operation)
		if e != nil {
			return e
		}
		return withCleanedCommitArchive(ctx, path, operation, b, files, h.ManifestHash, current, func(h RuntimeRecoveryCleanedCommitArchivedHistory, _ func() bool) error { return fn(h) })
	})
}
