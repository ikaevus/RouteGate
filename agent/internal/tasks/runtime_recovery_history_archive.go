package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

const recoveryHistoryArchivePurpose = "runtime_mutation_independent_recovery_history_v1"
const recoveryHistoryArchiveManifest = "manifest.json"
const recoveryHistoryArchiveLimit = 4 * runtimeResultLimit
const recoveryHistoryArchiveLock = "read-only recovery history lock v1\n"

type recoveryHistoryArchiveFile struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Hash  string `json:"hash"`
}
type recoveryHistoryArchiveRecord struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Purpose       string                       `json:"purpose"`
	OperationID   string                       `json:"operationId"`
	Binding       RuntimeRecoveryGrantBinding  `json:"binding"`
	RetentionHash string                       `json:"retentionHash"`
	ProofHash     string                       `json:"proofHash"`
	ReceiptHash   string                       `json:"receiptHash"`
	Files         []recoveryHistoryArchiveFile `json:"files"`
}
type recoveryHistoryCopyFile struct {
	metadata recoveryHistoryArchiveFile
	data     []byte
}

func recoveryHistoryArchivePath(dir, operation string) (string, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" || !canonicalTaskIDPattern.MatchString(operation) {
		return "", ErrRuntimeMutationBlocked
	}
	return filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-history-"+operation), nil
}

func privateRecoveryHistoryDir(path string, parent bool) (os.FileInfo, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, ErrRuntimeMutationBlocked
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || s.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0022 != 0 || (!parent && info.Mode().Perm() != 0700) {
		return nil, ErrRuntimeMutationBlocked
	}
	return info, nil
}

// Fixed validated paths only. Never interpret manifest-supplied paths. The
// original admission lock is excluded; a copy gets a separate read lock stub.
func collectRecoveryHistory(dir, operation string) ([]recoveryHistoryCopyFile, func() bool, error) {
	deny := func() ([]recoveryHistoryCopyFile, func() bool, error) { return nil, nil, ErrRuntimeMutationBlocked }
	root, e := privateRecoveryHistoryDir(dir, false)
	if e != nil {
		return deny()
	}
	archive, e := retirementArchivePath(dir, operation)
	if e != nil || privateRetirementArchive(archive) != nil {
		return deny()
	}
	arch, e := os.Lstat(archive)
	if e != nil {
		return deny()
	}
	names := []string{runtimeRecoveryRetirementFile, runtimeRecoveryTerminalFile, runtimeRecoveryTerminalSubmissionFile, runtimeRecoveryTerminalAcknowledgementFile, "mutation-inflight.json", runtimeRecoveryReleaseIntentFile, runtimeRecoveryPreparedSubmissionFile, runtimeRecoveryPreparedAcknowledgementFile, runtimeRecoveryReleaseRetentionFile, runtimeRecoveryRetentionSubmissionFile, runtimeRecoveryRetentionAcknowledgementFile}
	limits := []int64{8192, 8192, 8192, 4096, 16384, 8192, 8192, 4096, runtimeRecoveryReleaseRetentionLimit, 8192, 4096}
	rootExact := func() bool {
		info, e := privateRecoveryHistoryDir(dir, false)
		if e != nil || !os.SameFile(root, info) {
			return false
		}
		entries, e := os.ReadDir(dir)
		if e != nil || len(entries) != len(names)+2 {
			return false
		}
		allowed := map[string]bool{"runtime.lock": true, filepath.Base(archive): true}
		for _, n := range names {
			allowed[n] = true
		}
		for _, v := range entries {
			if !allowed[v.Name()] {
				return false
			}
		}
		return true
	}
	if !rootExact() {
		return deny()
	}
	var files []recoveryHistoryCopyFile
	var sources []releaseRetentionSource
	total := 0
	add := func(scope, base, name string, limit int64) bool {
		if limit <= 0 {
			return false
		}
		path := filepath.Join(base, name)
		info, e := os.Lstat(path)
		if e != nil {
			return false
		}
		data, e := readTerminalConfirmationFile(path, limit)
		after, e2 := os.Lstat(path)
		if e != nil || e2 != nil || !os.SameFile(info, after) {
			return false
		}
		total += len(data)
		if total > recoveryHistoryArchiveLimit {
			return false
		}
		files = append(files, recoveryHistoryCopyFile{recoveryHistoryArchiveFile{scope, name, markerDigest(data)}, data})
		sources = append(sources, releaseRetentionSource{path, limit, info, data})
		return true
	}
	for i, name := range names {
		if !add("root", dir, name, limits[i]) {
			return deny()
		}
	}
	entries, e := os.ReadDir(archive)
	if e != nil || len(entries) > 9 {
		return deny()
	}
	for _, v := range entries {
		if !add("archive", archive, v.Name(), retirementSourceLimit(v.Name())) {
			return deny()
		}
	}
	unchanged := func() bool {
		a, e := os.Lstat(archive)
		if e != nil || !os.SameFile(arch, a) || !rootExact() || privateRetirementArchive(archive) != nil {
			return false
		}
		for _, v := range sources {
			info, e := os.Lstat(v.path)
			data, e2 := readTerminalConfirmationFile(v.path, v.limit)
			after, e3 := os.Lstat(v.path)
			if e != nil || e2 != nil || e3 != nil || !os.SameFile(v.info, info) || !os.SameFile(v.info, after) || !bytes.Equal(data, v.data) {
				return false
			}
		}
		return true
	}
	if !unchanged() {
		return deny()
	}
	return files, unchanged, nil
}

func historyArchiveRecord(b RuntimeRecoveryGrantBinding, h RuntimeRecoveryRetentionHistory, files []recoveryHistoryCopyFile) recoveryHistoryArchiveRecord {
	p, _ := json.Marshal(h.Proof)
	ack, _ := json.Marshal(h.LocalReceipt)
	r := recoveryHistoryArchiveRecord{SchemaVersion: 1, Purpose: recoveryHistoryArchivePurpose, OperationID: h.Proof.OperationID, Binding: b, RetentionHash: h.Proof.RetentionHash, ProofHash: markerDigest(p), ReceiptHash: markerDigest(ack)}
	for _, f := range files {
		r.Files = append(r.Files, f.metadata)
	}
	return r
}

// Validate the independent copy using the existing complete historical chain.
// Caller owns the ORIGINAL admission flock throughout this call and callback.
func withRecoveryHistoryArchive(ctx context.Context, path string, b RuntimeRecoveryGrantBinding, operation string, fn func(RuntimeRecoveryRetentionHistory, string, func() bool) error) error {
	manifestPath := filepath.Join(path, recoveryHistoryArchiveManifest)
	outer, e := privateRecoveryHistoryDir(path, false)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	outerExact := func() bool {
		info, e := privateRecoveryHistoryDir(path, false)
		if e != nil || !os.SameFile(outer, info) {
			return false
		}
		entries, e := os.ReadDir(path)
		if e != nil || len(entries) != 2 {
			return false
		}
		return entries[0].Name() == "history" && entries[1].Name() == recoveryHistoryArchiveManifest
	}
	if !outerExact() {
		return ErrRuntimeMutationBlocked
	}
	manifestInfo, e := os.Lstat(manifestPath)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	data, e := readTerminalConfirmationFile(manifestPath, 16384)
	var r recoveryHistoryArchiveRecord
	if e != nil || !exactHistoricalJSON(data, &r) || r.OperationID != operation || r.Binding != b {
		return ErrRuntimeMutationBlocked
	}
	history := filepath.Join(path, "history")
	lockPath := filepath.Join(history, "runtime.lock")
	lockInfo, e := os.Lstat(lockPath)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	lockBytes, e := readTerminalConfirmationFile(lockPath, int64(len(recoveryHistoryArchiveLock)))
	if e != nil || string(lockBytes) != recoveryHistoryArchiveLock {
		return ErrRuntimeMutationBlocked
	}
	return WithHistoricalRecoveryRetention(ctx, history, b, func(h RuntimeRecoveryRetentionHistory) error {
		if h.Proof.OperationID != operation || !h.LocalReceipt.Recorded {
			return ErrRuntimeMutationBlocked
		}
		files, sources, e := collectRecoveryHistory(history, operation)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		expected, e := json.Marshal(historyArchiveRecord(b, h, files))
		if e != nil || !bytes.Equal(expected, data) {
			return ErrRuntimeMutationBlocked
		}
		unchanged := func() bool {
			info, e := os.Lstat(manifestPath)
			wire, e2 := readTerminalConfirmationFile(manifestPath, 16384)
			after, e3 := os.Lstat(manifestPath)
			lock, e4 := os.Lstat(lockPath)
			stub, e5 := readTerminalConfirmationFile(lockPath, int64(len(recoveryHistoryArchiveLock)))
			lockAfter, e6 := os.Lstat(lockPath)
			return e == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil && e6 == nil && outerExact() && sources() && os.SameFile(manifestInfo, info) && os.SameFile(manifestInfo, after) && bytes.Equal(wire, data) && os.SameFile(lockInfo, lock) && os.SameFile(lockInfo, lockAfter) && string(stub) == recoveryHistoryArchiveLock
		}
		if !unchanged() || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		if fn(h, markerDigest(data), unchanged) != nil || !unchanged() || ctx.Err() != nil {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}

// Copies only; no source is moved/deleted and this digest grants no release.
// Current authenticated Manager readiness must be checked in this callback first.
func (g *RuntimeRecoveryPreparedReleaseGuard) ArchiveRetainedHistory(ctx context.Context) (string, error) {
	return g.archiveRetainedHistory(ctx, nil)
}
func (g *RuntimeRecoveryPreparedReleaseGuard) archiveRetainedHistory(ctx context.Context, checkpoint func(string) error) (string, error) {
	if g == nil || g.state == nil {
		return "", ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	m := g.state.owner
	if m == nil || m.retentionReceipt == nil {
		return "", ErrRuntimeMutationBlocked
	}
	deny := func() (string, error) { m.poisoned = true; return "", ErrRuntimeMutationBlocked }
	p, e := m.retentionProofLocked(ctx)
	if e != nil {
		return deny()
	}
	path, e := recoveryHistoryArchivePath(m.dir, p.OperationID)
	if e != nil {
		return deny()
	}
	parent, e := privateRecoveryHistoryDir(filepath.Dir(m.dir), true)
	if e != nil {
		return deny()
	}
	parentCurrent := func() bool {
		now, e := privateRecoveryHistoryDir(filepath.Dir(m.dir), true)
		return e == nil && os.SameFile(parent, now)
	}
	if m.historyArchive != nil {
		if m.historyArchiveUnchanged == nil || !m.historyArchiveUnchanged() {
			return deny()
		}
		return markerDigest(m.historyArchive), nil
	}
	files, sourceCurrent, e := collectRecoveryHistory(m.dir, p.OperationID)
	if e != nil {
		return deny()
	}
	var ack RuntimeRecoveryRetentionReceipt
	if !exactHistoricalJSON(m.retentionReceipt, &ack) || !retentionReceiptMatches(ack, p, m.retentionSubmission) {
		return deny()
	}
	manifest, e := json.Marshal(historyArchiveRecord(m.binding, RuntimeRecoveryRetentionHistory{p, ack}, files))
	if e != nil || len(manifest) > 16384 {
		return deny()
	}
	current := func() bool {
		_, e := m.retentionProofLocked(ctx)
		return e == nil && sourceCurrent() && parentCurrent() && m.terminalAuthorityCurrent(ctx)
	}
	cut := func(step string) bool { return checkpoint == nil || checkpoint(step) == nil }
	if !current() || os.Mkdir(path, 0700) != nil {
		return deny()
	}
	ownedOuter, e := privateRecoveryHistoryDir(path, false)
	if e != nil {
		return deny()
	}
	if syncRetirementDir(filepath.Dir(path)) != nil || !cut("directory") {
		return deny()
	}
	history := filepath.Join(path, "history")
	archive := filepath.Join(history, "retired-"+p.OperationID)
	if os.Mkdir(history, 0700) != nil || os.Mkdir(archive, 0700) != nil || writeRemovalFileNoReplace(filepath.Join(history, "runtime.lock"), []byte(recoveryHistoryArchiveLock)) != nil {
		return deny()
	}
	ownedHistory, e := privateRecoveryHistoryDir(history, false)
	if e != nil {
		return deny()
	}
	ownedArchive, e := privateRecoveryHistoryDir(archive, false)
	if e != nil {
		return deny()
	}
	ownedLock, e := os.Lstat(filepath.Join(history, "runtime.lock"))
	if e != nil {
		return deny()
	}
	ownedDirectories := func() bool {
		o, e := privateRecoveryHistoryDir(path, false)
		h, e2 := privateRecoveryHistoryDir(history, false)
		a, e3 := privateRecoveryHistoryDir(archive, false)
		l, e4 := os.Lstat(filepath.Join(history, "runtime.lock"))
		data, e5 := readTerminalConfirmationFile(filepath.Join(history, "runtime.lock"), int64(len(recoveryHistoryArchiveLock)))
		return e == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil && os.SameFile(ownedOuter, o) && os.SameFile(ownedHistory, h) && os.SameFile(ownedArchive, a) && os.SameFile(ownedLock, l) && string(data) == recoveryHistoryArchiveLock
	}
	for _, f := range files {
		base := history
		if f.metadata.Scope == "archive" {
			base = archive
		}
		if !current() || !ownedDirectories() || writeRemovalFileNoReplace(filepath.Join(base, f.metadata.Name), f.data) != nil || !cut("copy-"+f.metadata.Scope+"-"+f.metadata.Name) {
			return deny()
		}
	}
	if WithHistoricalRecoveryRetention(ctx, history, m.binding, func(h RuntimeRecoveryRetentionHistory) error {
		if h.Proof != p || h.LocalReceipt != ack {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}) != nil || !current() || !ownedDirectories() {
		return deny()
	}
	_, copiedCurrent, e := collectRecoveryHistory(history, p.OperationID)
	if e != nil || !cut("copied") || !copiedCurrent() || !ownedDirectories() || !current() {
		return deny()
	}
	// Publish LAST. A crash before this record leaves an unusable partial copy.
	manifestPath := filepath.Join(path, recoveryHistoryArchiveManifest)
	if writeRemovalFileNoReplace(manifestPath, manifest) != nil {
		return deny()
	}
	ownedManifest, e := os.Lstat(manifestPath)
	if e != nil || !cut("published") || !ownedDirectories() || !copiedCurrent() {
		return deny()
	}
	manifestCurrent := func() bool {
		info, err := os.Lstat(manifestPath)
		data, readErr := readTerminalConfirmationFile(manifestPath, 16384)
		after, statErr := os.Lstat(manifestPath)
		return err == nil && readErr == nil && statErr == nil && os.SameFile(ownedManifest, info) && os.SameFile(ownedManifest, after) && bytes.Equal(data, manifest)
	}
	if !manifestCurrent() {
		return deny()
	}
	var unchanged func() bool
	if withRecoveryHistoryArchive(ctx, path, m.binding, p.OperationID, func(h RuntimeRecoveryRetentionHistory, hash string, stable func() bool) error {
		if h.Proof != p || h.LocalReceipt != ack || hash != markerDigest(manifest) {
			return ErrRuntimeMutationBlocked
		}
		unchanged = stable
		return nil
	}) != nil || !current() || !copiedCurrent() || !ownedDirectories() || !manifestCurrent() || unchanged == nil || !unchanged() {
		return deny()
	}
	m.historyArchive = manifest
	m.historyArchiveUnchanged = func() bool { return parentCurrent() && manifestCurrent() && unchanged() }
	if _, e = m.retentionProofLocked(ctx); e != nil {
		return deny()
	}
	return markerDigest(manifest), nil
}

type RuntimeRecoveryArchivedHistory struct {
	History      RuntimeRecoveryRetentionHistory
	ManifestHash string
}

// Read-only independent history under the original admission lock, even when
// original evidence files no longer exist. Never create/repair/resume anything.
func WithArchivedRecoveryRetention(ctx context.Context, dir, operation string, b RuntimeRecoveryGrantBinding, fn func(RuntimeRecoveryArchivedHistory) error) error {
	if ctx.Err() != nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	path, e := recoveryHistoryArchivePath(dir, operation)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	lock, e := lockRuntimeMutationExisting(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	defer lock.Close()
	root, e := privateRecoveryHistoryDir(dir, false)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	parent, e := privateRecoveryHistoryDir(filepath.Dir(dir), true)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	current := func() bool {
		r, e := privateRecoveryHistoryDir(dir, false)
		p, e2 := privateRecoveryHistoryDir(filepath.Dir(dir), true)
		info, e3 := os.Lstat(filepath.Join(dir, "runtime.lock"))
		held, e4 := lock.Stat()
		if e != nil || e2 != nil || e3 != nil || e4 != nil || !os.SameFile(root, r) || !os.SameFile(parent, p) || !os.SameFile(info, held) {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		return ok && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0 && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
	}
	if !current() {
		return ErrRuntimeMutationBlocked
	}
	return withRecoveryHistoryArchive(ctx, path, b, operation, func(h RuntimeRecoveryRetentionHistory, hash string, _ func() bool) error {
		if !current() || fn(RuntimeRecoveryArchivedHistory{h, hash}) != nil || !current() {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}
