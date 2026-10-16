package tasks

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// A read-only local inventory, not wire evidence, a durable intent or permission
// to delete anything. A future release protocol must revalidate under the live
// owner lock, preserve the archive and make every crash cut fail closed.
type RuntimeRecoveryFenceReleasePlan struct {
	OperationID, AuthorizationHash, TerminalProofHash, TerminalReceiptHash string
	Binding                                                                RuntimeRecoveryGrantBinding
	ExpiresAt                                                              time.Time
	ArchiveName                                                            string
	Files                                                                  [5]RuntimeRecoveryFenceReleaseFile
}

type RuntimeRecoveryFenceReleaseFile struct {
	Name, Hash string
}

type releasePlanFileSnapshot struct {
	info os.FileInfo
	file RuntimeRecoveryFenceReleaseFile
}

// PlanFenceRelease requires the original live owner and its durable terminal
// acknowledgement, exact completed archival cleanup and a fresh runtime witness.
// It never writes, removes, resumes or releases state. The returned value cannot
// carry lock ownership beyond this call and no API consumes it as authority.
// Call outside submission callbacks, which already hold the owner mutex.
func (m *RuntimeRecoveryFinalization) PlanFenceRelease(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string) (RuntimeRecoveryFenceReleasePlan, error) {
	var zero RuntimeRecoveryFenceReleasePlan
	if m == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.planFenceReleaseLocked(ctx, b, authorizationHash)
}

func (m *RuntimeRecoveryFinalization) planFenceReleaseLocked(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string) (RuntimeRecoveryFenceReleasePlan, error) {
	var zero RuntimeRecoveryFenceReleasePlan
	if m.binding != b || m.authorizationHash != authorizationHash || m.terminalSubmission == nil || m.terminalAcknowledgement == nil {
		return zero, ErrRuntimeMutationBlocked
	}
	deny := func() (RuntimeRecoveryFenceReleasePlan, error) {
		m.poisoned = true
		return zero, ErrRuntimeMutationBlocked
	}
	if _, e := m.terminalProofLocked(ctx); e != nil {
		return deny()
	}
	archive, e := retirementArchivePath(m.dir, m.terminalFrozen.OperationID)
	if e != nil || archive != m.archive {
		return deny()
	}
	names := [5]string{runtimeRecoveryRetirementFile, runtimeRecoveryTerminalFile, runtimeRecoveryTerminalSubmissionFile, runtimeRecoveryTerminalAcknowledgementFile, "mutation-inflight.json"}
	limits := [5]int64{8192, 8192, 8192, 4096, 16384}
	// Unlike historical readback, readiness requires the exact final root set:
	// no redundant active receipt, partial cleanup, temp file or foreign archive.
	inventory := func() ([6]releasePlanFileSnapshot, os.FileInfo, os.FileInfo, error) {
		var files [6]releasePlanFileSnapshot
		fail := func() ([6]releasePlanFileSnapshot, os.FileInfo, os.FileInfo, error) {
			return files, nil, nil, ErrRuntimeMutationBlocked
		}
		entries, e := os.ReadDir(m.dir)
		expectedCount := len(names) + 2
		if m.releaseIntent != nil {
			expectedCount++
		}
		if e != nil || len(entries) != expectedCount {
			return fail()
		}
		allowed := map[string]bool{"runtime.lock": true, filepath.Base(archive): true}
		if m.releaseIntent != nil {
			allowed[runtimeRecoveryReleaseIntentFile] = true
		}
		for _, name := range names {
			allowed[name] = true
		}
		for _, entry := range entries {
			if !allowed[entry.Name()] {
				return fail()
			}
		}
		root, e := os.Lstat(m.dir)
		if e != nil {
			return fail()
		}
		stat, ok := root.Sys().(*syscall.Stat_t)
		if !ok || !root.IsDir() || root.Mode().Perm() != 0700 || stat.Uid != uint32(os.Geteuid()) {
			return fail()
		}
		arch, e := os.Lstat(archive)
		if e != nil || privateRetirementArchive(archive) != nil {
			return fail()
		}
		lock, e := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
		held, heldErr := m.lock.Stat()
		if e != nil || heldErr != nil || !os.SameFile(lock, held) {
			return fail()
		}
		lockStat, ok := lock.Sys().(*syscall.Stat_t)
		if !ok || !lock.Mode().IsRegular() || lock.Mode().Perm()&0077 != 0 || lockStat.Uid != uint32(os.Geteuid()) || lockStat.Nlink != 1 {
			return fail()
		}
		for i, name := range names {
			path := filepath.Join(m.dir, name)
			info, e := os.Lstat(path)
			if e != nil {
				return fail()
			}
			data, e := readTerminalConfirmationFile(path, limits[i])
			after, afterErr := os.Lstat(path)
			if e != nil || afterErr != nil || !os.SameFile(info, after) {
				return fail()
			}
			files[i] = releasePlanFileSnapshot{info, RuntimeRecoveryFenceReleaseFile{name, markerDigest(data)}}
		}
		if m.releaseIntent != nil {
			path := filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)
			info, e := os.Lstat(path)
			if e != nil || !releaseIntentMatchesOwner(m.dir, m.releaseIntent) {
				return fail()
			}
			after, e := os.Lstat(path)
			if e != nil || !os.SameFile(info, after) {
				return fail()
			}
			files[5] = releasePlanFileSnapshot{info, RuntimeRecoveryFenceReleaseFile{runtimeRecoveryReleaseIntentFile, markerDigest(m.releaseIntent)}}
		}
		return files, root, arch, nil
	}
	before, root, arch, e := inventory()
	if e != nil {
		return deny()
	}
	// Both complete history and live process/config/listener are checked again
	// between inventories; neither a stored receipt nor heartbeat replaces this.
	if _, e = m.terminalProofLocked(ctx); e != nil {
		return deny()
	}
	after, rootAfter, archAfter, e := inventory()
	if e != nil || !os.SameFile(root, rootAfter) || !os.SameFile(arch, archAfter) {
		return deny()
	}
	p := RuntimeRecoveryFenceReleasePlan{OperationID: m.terminalFrozen.OperationID, AuthorizationHash: m.authorizationHash,
		TerminalProofHash: markerDigest(m.terminalSubmission), TerminalReceiptHash: markerDigest(m.terminalAcknowledgement),
		Binding: m.binding, ExpiresAt: m.authorization.ExpiresAt, ArchiveName: filepath.Base(archive)}
	for i := range before {
		if i == 5 && m.releaseIntent == nil {
			continue
		}
		if before[i].file != after[i].file || !os.SameFile(before[i].info, after[i].info) {
			return deny()
		}
		if i < len(p.Files) {
			p.Files[i] = before[i].file
		}
	}
	if !m.terminalAuthorityCurrent(ctx) {
		return deny()
	}
	return p, nil
}
