package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
)

const runtimeRecoverySubmissionFile = "recovery-submission.json"

func runtimeRecoveryStateAbsent(dir string) bool {
	return runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryConsumptionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoverySubmissionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryAcknowledgementFile))
}

func (m *RuntimeRecoveryCommit) submissionUnchanged() bool {
	if !m.acknowledgementUnchanged() {
		return false
	}
	path := filepath.Join(m.dir, runtimeRecoverySubmissionFile)
	if m.submission == nil {
		return runtimeMarkerAbsent(path)
	}
	data, err := readRemovalFile(path, 8192, true)
	return err == nil && bytes.Equal(data, m.submission)
}

// Guard exposes only measured read-only proof while its owner mutex/flock stay
// held across the synchronous callback. Escaped and zero guards fail closed.
// Callers must use this guard rather than calling owner methods in the callback.
type RuntimeRecoveryCommitGuard struct {
	state *runtimeRecoveryCommitGuardState
}

// Copies share the same expiring state; copying the public guard cannot retain
// an owner reference after WithSubmission has ended.
type runtimeRecoveryCommitGuardState struct {
	mu    sync.Mutex
	owner *RuntimeRecoveryCommit
}

func (g *RuntimeRecoveryCommitGuard) Proof(ctx context.Context) (RuntimeRecoveryCommitProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryCommitProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryCommitProof{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.proofLocked(ctx)
}

// PinProof durably fixes exact submission bytes before HTTP. Subsequent calls
// re-observe but return the same original proof. Nothing is overwritten/resumed.
func (g *RuntimeRecoveryCommitGuard) PinProof(ctx context.Context) (RuntimeRecoveryCommitProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryCommitProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryCommitProof{}, ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	p, err := m.proofLocked(ctx)
	if err != nil {
		return RuntimeRecoveryCommitProof{}, err
	}
	if m.submission != nil {
		return m.frozen, nil
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > 8192 || writeRemovalFile(filepath.Join(m.dir, runtimeRecoverySubmissionFile), data) != nil {
		m.poisoned = true
		return RuntimeRecoveryCommitProof{}, ErrRuntimeMutationBlocked
	}
	m.submission = data
	m.frozen = p
	if _, err = m.proofLocked(ctx); err != nil {
		return RuntimeRecoveryCommitProof{}, err
	}
	return p, nil
}

// WithSubmission retains the live owner through the callback and a final fresh
// observation. Close cannot interrupt HTTP. No acknowledgement can unlock it.
func (m *RuntimeRecoveryCommit) WithSubmission(ctx context.Context, binding RuntimeRecoveryGrantBinding, grantHash string, fn func(*RuntimeRecoveryCommitGuard) error) error {
	if m == nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binding != binding || m.grantHash != grantHash {
		return ErrRuntimeMutationBlocked
	}
	g := &RuntimeRecoveryCommitGuard{state: &runtimeRecoveryCommitGuardState{owner: m}}
	defer func() { g.state.mu.Lock(); g.state.owner = nil; g.state.mu.Unlock() }()
	if _, err := g.Proof(ctx); err != nil {
		return err
	}
	err := fn(g)
	if _, finalErr := g.Proof(ctx); finalErr != nil {
		return finalErr
	}
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	return nil
}
