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

const runtimeRecoveryTerminalFile = "recovery-retirement-terminal.json"
const runtimeRecoveryTerminalPurpose = "runtime_mutation_retirement_archive_v1"

// Archived history only. Both root journal and terminal record remain admission
// barriers. Neither the record nor its digest is permission to release a fence.
type runtimeRecoveryTerminalRecord struct {
	SchemaVersion    int                         `json:"schemaVersion"`
	Purpose          string                      `json:"purpose"`
	OperationID      string                      `json:"operationId"`
	Binding          RuntimeRecoveryGrantBinding `json:"binding"`
	IntentHash       string                      `json:"intentHash"`
	FinalProofHash   string                      `json:"finalProofHash"`
	FinalReceiptHash string                      `json:"finalReceiptHash"`
	ObservedAt       time.Time                   `json:"observedAt"`
	Observation      RuntimeRecoveryObservation  `json:"observation"`
}

func retirementArchivePath(dir, operation string) (string, error) {
	if !filepath.IsAbs(dir) || !canonicalTaskIDPattern.MatchString(operation) {
		return "", ErrRuntimeMutationBlocked
	}
	return filepath.Join(dir, "retired-"+operation), nil
}
func retirementSourceLimit(name string) int64 {
	switch name {
	case "mutation-inflight.json":
		return 16384
	case runtimeResultFile:
		return runtimeResultLimit
	case runtimeRecoveryConsumptionFile, runtimeRecoveryFinalConsumptionFile:
		return runtimeRecoveryConsumptionLimit
	case runtimeRecoverySubmissionFile, runtimeRecoveryFinalSubmissionFile, runtimeRecoveryRetirementFile:
		return 8192
	case runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalAcknowledgementFile:
		return 4096
	}
	return 0
}
func syncRetirementDir(dir string) error {
	f, e := os.Open(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	defer f.Close()
	if f.Sync() != nil {
		return ErrRuntimeMutationBlocked
	}
	return nil
}

func privateRetirementArchive(dir string) error {
	info, e := os.Lstat(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return ErrRuntimeMutationBlocked
	}
	files, e := retirementSourceManifest(dir)
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	allowed := map[string]bool{runtimeRecoveryRetirementFile: true}
	for _, file := range files {
		if file.Present {
			allowed[file.Name] = true
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != len(allowed) {
		return ErrRuntimeMutationBlocked
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return ErrRuntimeMutationBlocked
		}
		info, e := os.Lstat(filepath.Join(dir, entry.Name()))
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0600 {
			return ErrRuntimeMutationBlocked
		}
	}
	return nil
}

func terminalRootSnapshot(dir string, intent runtimeRecoveryRetirementIntent) (func() bool, error) {
	if !runtimeMarkerAbsent(filepath.Join(dir, "inflight.json")) {
		return nil, ErrRuntimeMutationBlocked
	}
	data := make([][]byte, len(intent.Files))
	for i, file := range intent.Files {
		path := filepath.Join(dir, file.Name)
		if runtimeMarkerAbsent(path) {
			continue
		}
		if !file.Present || retirementSourceLimit(file.Name) == 0 {
			return nil, ErrRuntimeMutationBlocked
		}
		var e error
		data[i], e = readRemovalFile(path, retirementSourceLimit(file.Name), true)
		if e != nil || markerDigest(data[i]) != file.Hash {
			return nil, ErrRuntimeMutationBlocked
		}
	}
	return func() bool {
		if !runtimeMarkerAbsent(filepath.Join(dir, "inflight.json")) {
			return false
		}
		for i, file := range intent.Files {
			path := filepath.Join(dir, file.Name)
			if data[i] == nil {
				if !runtimeMarkerAbsent(path) {
					return false
				}
				continue
			}
			current, e := readRemovalFile(path, retirementSourceLimit(file.Name), true)
			if e != nil || !bytes.Equal(data[i], current) {
				return false
			}
		}
		return true
	}, nil
}

// Stable outer admission flock is never moved into the archive or recreated.
// Leftover active files may be present after a crash, but must match the exact
// archived manifest. Historical confirmation never finishes their cleanup.
func withTerminalRecoveryHistoryLocked(ctx context.Context, dir string, b RuntimeRecoveryGrantBinding, lock *os.File, fn func(RuntimeRecoveryFinalizationProof) error) error {
	data, e := readRemovalFile(filepath.Join(dir, runtimeRecoveryTerminalFile), 8192, true)
	var terminal runtimeRecoveryTerminalRecord
	if e != nil || !exactHistoricalJSON(data, &terminal) || terminal.SchemaVersion != 1 || terminal.Purpose != runtimeRecoveryTerminalPurpose || terminal.Binding != b {
		return ErrRuntimeMutationBlocked
	}
	archive, e := retirementArchivePath(dir, terminal.OperationID)
	if e != nil || privateRetirementArchive(archive) != nil {
		return ErrRuntimeMutationBlocked
	}
	rootIntent, e := readRemovalFile(filepath.Join(dir, runtimeRecoveryRetirementFile), 8192, true)
	if e != nil || markerDigest(rootIntent) != terminal.IntentHash {
		return ErrRuntimeMutationBlocked
	}
	return withHistoricalFinalizationLocked(ctx, archive, b, lock, func(p RuntimeRecoveryFinalizationProof) error {
		pin, e := readRemovalFile(filepath.Join(archive, runtimeRecoveryFinalSubmissionFile), 8192, true)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		ack, e := readRemovalFile(filepath.Join(archive, runtimeRecoveryFinalAcknowledgementFile), 4096, true)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		intentData, e := readRemovalFile(filepath.Join(archive, runtimeRecoveryRetirementFile), 8192, true)
		var intent runtimeRecoveryRetirementIntent
		consumption, e2 := readRemovalFile(filepath.Join(archive, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit, true)
		var consumed runtimeRecoveryFinalConsumption
		if e != nil || e2 != nil || !bytes.Equal(intentData, rootIntent) || !exactHistoricalJSON(intentData, &intent) || !exactHistoricalJSON(consumption, &consumed) || terminal.OperationID != p.OperationID || terminal.FinalProofHash != markerDigest(pin) || terminal.FinalReceiptHash != markerDigest(ack) || terminal.ObservedAt.Before(intent.ObservedAt) || !terminal.ObservedAt.Before(consumed.Authorization.ExpiresAt) || terminal.Observation != p.Observation {
			return ErrRuntimeMutationBlocked
		}
		unchanged, e := terminalRootSnapshot(dir, intent)
		if e != nil {
			return ErrRuntimeMutationBlocked
		}
		fencesUnchanged := func() bool {
			t, e := readRemovalFile(filepath.Join(dir, runtimeRecoveryTerminalFile), 8192, true)
			j, e2 := readRemovalFile(filepath.Join(dir, runtimeRecoveryRetirementFile), 8192, true)
			return e == nil && e2 == nil && bytes.Equal(t, data) && bytes.Equal(j, rootIntent) && unchanged() && privateRetirementArchive(archive) == nil
		}
		if ctx.Err() != nil || !fencesUnchanged() || fn(p) != nil || ctx.Err() != nil || !fencesUnchanged() {
			return ErrRuntimeMutationBlocked
		}
		return nil
	})
}

// RetirePrepared archives the exact state before removing redundant active
// copies. Journal + terminal barrier remain; there is no Agent/Manager release.
// Only the original live owner may perform it; no crash resume is provided.
// Call after WithSubmission has returned, never inside its callback.
func (m *RuntimeRecoveryFinalization) RetirePrepared(ctx context.Context) (string, error) {
	return m.retirePrepared(ctx, nil)
}
func (m *RuntimeRecoveryFinalization) retirePrepared(ctx context.Context, checkpoint func(string) error) (string, error) {
	if m == nil {
		return "", ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	deny := func() (string, error) { m.poisoned = true; return "", ErrRuntimeMutationBlocked }
	step := func(name string) error {
		if checkpoint != nil && checkpoint(name) != nil {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}
	if m.terminal != nil {
		if _, e := m.terminalObservation(ctx); e != nil {
			return deny()
		}
		return markerDigest(m.terminal), nil
	}
	if _, e := m.proofLocked(ctx); e != nil {
		return deny()
	}
	if m.retirement == nil || m.submission == nil || m.acknowledgement == nil {
		return "", ErrRuntimeMutationBlocked
	}
	intent, e := validateRetirementIntent(m.retirement, m.dir, m.binding, m.authorization, m.frozen, m.submission, m.acknowledgement)
	if e != nil {
		return deny()
	}
	archive, e := retirementArchivePath(m.dir, intent.OperationID)
	if e != nil || !runtimeMarkerAbsent(archive) {
		return deny()
	}
	staging, e := os.MkdirTemp(m.dir, ".retirement-")
	if e != nil || syncRetirementDir(m.dir) != nil {
		return deny()
	}
	// No automatic removal on failure: original state/barriers remain, and any
	// partial private staging is retained for explicit future reconciliation.
	for _, file := range intent.Files {
		if !file.Present {
			continue
		}
		if _, e = m.proofLocked(ctx); e != nil {
			return deny()
		}
		data, e := readRemovalFile(filepath.Join(m.dir, file.Name), retirementSourceLimit(file.Name), true)
		if e != nil || markerDigest(data) != file.Hash || writeRemovalFile(filepath.Join(staging, file.Name), data) != nil || step("copy-"+file.Name) != nil {
			return deny()
		}
	}
	if writeRemovalFile(filepath.Join(staging, runtimeRecoveryRetirementFile), m.retirement) != nil || privateRetirementArchive(staging) != nil || syncRetirementDir(staging) != nil {
		return deny()
	}
	if e = withHistoricalFinalizationLocked(ctx, staging, m.binding, m.lock, func(p RuntimeRecoveryFinalizationProof) error {
		if p != m.frozen {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}); e != nil {
		return deny()
	}
	if _, e = m.proofLocked(ctx); e != nil || !runtimeMarkerAbsent(archive) {
		return deny()
	}
	if os.Rename(staging, archive) != nil || syncRetirementDir(m.dir) != nil || step("archive") != nil {
		return deny()
	}
	m.archive = archive
	if privateRetirementArchive(archive) != nil {
		return deny()
	}
	if e = withHistoricalFinalizationLocked(ctx, archive, m.binding, m.lock, func(p RuntimeRecoveryFinalizationProof) error {
		if p != m.frozen {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}); e != nil {
		return deny()
	}
	p, e := m.proofLocked(ctx)
	if e != nil {
		return deny()
	}
	terminal := runtimeRecoveryTerminalRecord{1, runtimeRecoveryTerminalPurpose, p.OperationID, m.binding, markerDigest(m.retirement), markerDigest(m.submission), markerDigest(m.acknowledgement), p.ObservedAt, p.Observation}
	data, e := json.Marshal(terminal)
	if e != nil || len(data) > 8192 || !m.terminalAuthorityCurrent(ctx) || writeRemovalFile(filepath.Join(m.dir, runtimeRecoveryTerminalFile), data) != nil {
		return deny()
	}
	m.terminal = data
	if step("terminal") != nil {
		return deny()
	}
	if _, e = m.terminalObservation(ctx); e != nil {
		return deny()
	}
	for _, file := range intent.Files {
		// Keep the original legacy marker for older Agent binaries too. Actual
		// fence release is a separate future protocol, never terminal archival.
		if !file.Present || file.Name == "mutation-inflight.json" {
			continue
		}
		if _, e = m.terminalObservation(ctx); e != nil {
			return deny()
		}
		path := filepath.Join(m.dir, file.Name)
		current, e := readRemovalFile(path, retirementSourceLimit(file.Name), true)
		if e != nil || markerDigest(current) != file.Hash || !m.terminalAuthorityCurrent(ctx) || removeRemovalFile(path) != nil || step("cleanup-"+file.Name) != nil {
			return deny()
		}
	}
	if _, e = m.terminalObservation(ctx); e != nil {
		return deny()
	}
	return markerDigest(m.terminal), nil
}

func (m *RuntimeRecoveryFinalization) terminalObservation(ctx context.Context) (time.Time, error) {
	deny := func() (time.Time, error) { return time.Time{}, ErrRuntimeMutationBlocked }
	if m.lock == nil || m.now == nil || m.poisoned || ctx.Err() != nil {
		return deny()
	}
	started := m.now().UTC()
	a := m.authorization
	if started.Before(m.lastChecked) || started.Before(a.IssuedAt) || !started.Before(a.ExpiresAt) {
		return deny()
	}
	check := func() error {
		return withTerminalRecoveryHistoryLocked(ctx, m.dir, m.binding, m.lock, func(p RuntimeRecoveryFinalizationProof) error {
			terminal, e := readRemovalFile(filepath.Join(m.dir, runtimeRecoveryTerminalFile), 8192, true)
			intent, e2 := readRemovalFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile), 8192, true)
			if e != nil || e2 != nil || p != m.frozen || !bytes.Equal(terminal, m.terminal) || !bytes.Equal(intent, m.retirement) {
				return ErrRuntimeMutationBlocked
			}
			return nil
		})
	}
	if check() != nil {
		return deny()
	}
	bounded, cancel := context.WithDeadline(ctx, a.ExpiresAt)
	defer cancel()
	w, e := CaptureRuntimeRecoveryWitness(bounded, m.activePath, m.adapter)
	w.PreviousGeneration = a.Expected.Witness.PreviousGeneration
	if e != nil || bounded.Err() != nil || w != a.Expected.Witness || check() != nil {
		return deny()
	}
	finished := m.now().UTC()
	if bounded.Err() != nil || finished.Before(started) || !finished.Before(a.ExpiresAt) {
		return deny()
	}
	m.lastChecked = finished
	return finished, nil
}

func (m *RuntimeRecoveryFinalization) terminalAuthorityCurrent(ctx context.Context) bool {
	if m.lock == nil || m.now == nil || m.poisoned || ctx.Err() != nil {
		return false
	}
	now := m.now().UTC()
	return !now.Before(m.lastChecked) && !now.Before(m.authorization.IssuedAt) && now.Before(m.authorization.ExpiresAt)
}
