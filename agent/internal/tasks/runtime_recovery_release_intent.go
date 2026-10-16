package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

const runtimeRecoveryReleaseIntentFile = "recovery-fence-release-intent.json"
const runtimeRecoveryReleaseIntentPurpose = "runtime_mutation_fence_release_prepared_v1"

// Private durable pre-deletion intent only. No consumer may interpret its
// existence as evidence that any barrier, archive or reservation was released.
type runtimeRecoveryReleaseIntent struct {
	SchemaVersion       int                                      `json:"schemaVersion"`
	Purpose             string                                   `json:"purpose"`
	OperationID         string                                   `json:"operationId"`
	Binding             RuntimeRecoveryGrantBinding              `json:"binding"`
	AuthorizationHash   string                                   `json:"authorizationHash"`
	TerminalProofHash   string                                   `json:"terminalProofHash"`
	TerminalReceiptHash string                                   `json:"terminalReceiptHash"`
	PreparedAt          time.Time                                `json:"preparedAt"`
	ExpiresAt           time.Time                                `json:"expiresAt"`
	ArchiveName         string                                   `json:"archiveName"`
	Files               [5]runtimeRecoveryReleaseIntentFileEntry `json:"files"`
}
type runtimeRecoveryReleaseIntentFileEntry struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

func releaseIntentForPlan(p RuntimeRecoveryFenceReleasePlan, prepared time.Time) runtimeRecoveryReleaseIntent {
	r := runtimeRecoveryReleaseIntent{SchemaVersion: 1, Purpose: runtimeRecoveryReleaseIntentPurpose, OperationID: p.OperationID, Binding: p.Binding,
		AuthorizationHash: p.AuthorizationHash, TerminalProofHash: p.TerminalProofHash, TerminalReceiptHash: p.TerminalReceiptHash,
		PreparedAt: prepared, ExpiresAt: p.ExpiresAt, ArchiveName: p.ArchiveName}
	for i, f := range p.Files {
		r.Files[i] = runtimeRecoveryReleaseIntentFileEntry{f.Name, f.Hash}
	}
	return r
}

// nil requires absence, never adoption of disk history. Only original live
// owner bytes permit read-only validation after this new phase was published.
func releaseIntentMatchesOwner(dir string, owned []byte) bool {
	path := filepath.Join(dir, runtimeRecoveryReleaseIntentFile)
	if owned == nil {
		return runtimeMarkerAbsent(path)
	}
	data, e := readTerminalConfirmationFile(path, 8192)
	return e == nil && bytes.Equal(data, owned)
}

// PrepareFenceRelease persists a private barrier before a future crash-safe
// deletion protocol. It derives its own fresh plan; caller-supplied inventory
// cannot authorize publication. No deletion, grant renewal, resume or unlock.
// Call outside submission callbacks, which already hold the owner mutex.
func (m *RuntimeRecoveryFinalization) PrepareFenceRelease(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string) (string, error) {
	return m.prepareFenceRelease(ctx, b, authorizationHash, nil)
}

func (m *RuntimeRecoveryFinalization) prepareFenceRelease(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string, checkpoint func() error) (string, error) {
	if m == nil {
		return "", ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	deny := func() (string, error) { m.poisoned = true; return "", ErrRuntimeMutationBlocked }
	p, e := m.planFenceReleaseLocked(ctx, b, authorizationHash)
	if e != nil {
		return "", ErrRuntimeMutationBlocked
	}
	if m.releaseIntent != nil {
		var saved runtimeRecoveryReleaseIntent
		if !exactHistoricalJSON(m.releaseIntent, &saved) || saved.PreparedAt.Before(m.terminalFrozen.ObservedAt) || saved.PreparedAt.After(m.lastChecked) || !saved.PreparedAt.Before(p.ExpiresAt) || saved != releaseIntentForPlan(p, saved.PreparedAt) {
			return deny()
		}
		return markerDigest(m.releaseIntent), nil
	}
	r := releaseIntentForPlan(p, m.lastChecked)
	if r.PreparedAt.Before(m.terminalFrozen.ObservedAt) || !r.PreparedAt.Before(r.ExpiresAt) {
		return deny()
	}
	data, e := json.Marshal(r)
	if e != nil || len(data) > 8192 || !m.terminalAuthorityCurrent(ctx) || writeRemovalFileNoReplace(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile), data) != nil {
		return deny()
	}
	// The file is already fsynced and remains on every subsequent failure.
	if checkpoint != nil && checkpoint() != nil {
		return deny()
	}
	m.releaseIntent = data
	after, e := m.planFenceReleaseLocked(ctx, b, authorizationHash)
	if e != nil || after != p {
		return deny()
	}
	return markerDigest(data), nil
}
