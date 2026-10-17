package tasks

import (
	"bytes"
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
	inventory := fenceReleaseInventory(m.dir, archive, m.lock, m.releaseIntent, m.preparedSubmission, m.preparedAcknowledgement, m.releaseHistoryRetention, m.retentionSubmission, m.retentionReceipt)
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
		if before[i].info == nil && after[i].info == nil {
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

func fenceReleaseInventory(dir, archive string, lock *os.File, releaseIntent []byte, confirmations ...[]byte) func() ([11]releasePlanFileSnapshot, os.FileInfo, os.FileInfo, error) {
	names := [5]string{runtimeRecoveryRetirementFile, runtimeRecoveryTerminalFile, runtimeRecoveryTerminalSubmissionFile, runtimeRecoveryTerminalAcknowledgementFile, "mutation-inflight.json"}
	limits := [5]int64{8192, 8192, 8192, 4096, 16384}
	// Unlike historical readback, readiness requires the exact final root set:
	// no redundant active receipt, partial cleanup, temp file or foreign archive.
	confirmationNames := []string{runtimeRecoveryPreparedSubmissionFile, runtimeRecoveryPreparedAcknowledgementFile, runtimeRecoveryReleaseRetentionFile, runtimeRecoveryRetentionSubmissionFile, runtimeRecoveryRetentionAcknowledgementFile}
	return func() ([11]releasePlanFileSnapshot, os.FileInfo, os.FileInfo, error) {
		var files [11]releasePlanFileSnapshot
		fail := func() ([11]releasePlanFileSnapshot, os.FileInfo, os.FileInfo, error) {
			return files, nil, nil, ErrRuntimeMutationBlocked
		}
		entries, e := os.ReadDir(dir)
		expectedCount := len(names) + 2
		if releaseIntent != nil {
			expectedCount++
		}
		// Each phase permits only its exact owned bytes. Older readers omit
		// later slots and still reject their additional root files.
		if len(confirmations) != 0 && len(confirmations) != 2 && len(confirmations) != 3 && len(confirmations) != 5 {
			return fail()
		}
		for _, data := range confirmations {
			if data != nil {
				expectedCount++
			}
		}
		if len(confirmations) >= 2 && confirmations[1] != nil && confirmations[0] == nil {
			return fail()
		}
		if len(confirmations) >= 2 && confirmations[0] != nil && releaseIntent == nil {
			return fail()
		}
		if len(confirmations) >= 3 && confirmations[2] != nil && (confirmations[0] == nil || confirmations[1] == nil) {
			return fail()
		}
		if len(confirmations) == 5 && ((confirmations[3] != nil && confirmations[2] == nil) || (confirmations[4] != nil && confirmations[3] == nil)) {
			return fail()
		}
		if e != nil || len(entries) != expectedCount {
			return fail()
		}
		allowed := map[string]bool{"runtime.lock": true, filepath.Base(archive): true}
		if releaseIntent != nil {
			allowed[runtimeRecoveryReleaseIntentFile] = true
		}
		for i, data := range confirmations {
			if data != nil {
				allowed[confirmationNames[i]] = true
			}
		}
		for _, name := range names {
			allowed[name] = true
		}
		for _, entry := range entries {
			if !allowed[entry.Name()] {
				return fail()
			}
		}
		root, e := os.Lstat(dir)
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
		lockInfo, e := os.Lstat(filepath.Join(dir, "runtime.lock"))
		held, heldErr := lock.Stat()
		if e != nil || heldErr != nil || !os.SameFile(lockInfo, held) {
			return fail()
		}
		lockStat, ok := lockInfo.Sys().(*syscall.Stat_t)
		if !ok || !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm()&0077 != 0 || lockStat.Uid != uint32(os.Geteuid()) || lockStat.Nlink != 1 {
			return fail()
		}
		for i, name := range names {
			path := filepath.Join(dir, name)
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
		if releaseIntent != nil {
			path := filepath.Join(dir, runtimeRecoveryReleaseIntentFile)
			info, e := os.Lstat(path)
			if e != nil || !releaseIntentMatchesSnapshot(dir, releaseIntent) {
				return fail()
			}
			after, e := os.Lstat(path)
			if e != nil || !os.SameFile(info, after) {
				return fail()
			}
			files[5] = releasePlanFileSnapshot{info, RuntimeRecoveryFenceReleaseFile{runtimeRecoveryReleaseIntentFile, markerDigest(releaseIntent)}}
		}
		for i, pinned := range confirmations {
			if pinned == nil {
				continue
			}
			name := confirmationNames[i]
			path := filepath.Join(dir, name)
			info, e := os.Lstat(path)
			limit := int64(8192)
			if i == 2 {
				limit = runtimeRecoveryReleaseRetentionLimit
			}
			data, readErr := readTerminalConfirmationFile(path, limit)
			after, afterErr := os.Lstat(path)
			if e != nil || readErr != nil || afterErr != nil || !os.SameFile(info, after) || !bytes.Equal(data, pinned) {
				return fail()
			}
			files[6+i] = releasePlanFileSnapshot{info, RuntimeRecoveryFenceReleaseFile{name, markerDigest(data)}}
		}
		return files, root, arch, nil
	}
}
