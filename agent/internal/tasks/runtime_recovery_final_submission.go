package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
)

// Guard exposes only measured read-only proof while its owner mutex/flock stay
// held across the synchronous callback. Escaped and zero guards fail closed.
// Callers must use this guard rather than calling owner methods in the callback.
type RuntimeRecoveryFinalizationGuard struct {
	state *runtimeRecoveryFinalizationGuardState
}

// Copies share the same expiring state; copying the public guard cannot retain
// an owner reference after WithSubmission has ended.
type runtimeRecoveryFinalizationGuardState struct {
	mu    sync.Mutex
	owner *RuntimeRecoveryFinalization
}

func (g *RuntimeRecoveryFinalizationGuard) Proof(ctx context.Context) (RuntimeRecoveryFinalizationProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.proofLocked(ctx)
}

// PinProof durably fixes exact submission bytes before HTTP. Subsequent calls
// re-observe but return the same original proof. Nothing is overwritten/resumed.
func (g *RuntimeRecoveryFinalizationGuard) PinProof(ctx context.Context) (RuntimeRecoveryFinalizationProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	p, err := m.proofLocked(ctx)
	if err != nil {
		return RuntimeRecoveryFinalizationProof{}, err
	}
	if m.submission != nil {
		return m.frozen, nil
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > 8192 || writeRemovalFile(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile), data) != nil {
		m.poisoned = true
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	m.submission = data
	m.frozen = p
	if _, err = m.proofLocked(ctx); err != nil {
		return RuntimeRecoveryFinalizationProof{}, err
	}
	return p, nil
}

// WithSubmission retains the live owner through the callback and a final fresh
// observation. Close cannot interrupt HTTP. No acknowledgement can unlock it.
func (m *RuntimeRecoveryFinalization) WithSubmission(ctx context.Context, binding RuntimeRecoveryGrantBinding, authorizationHash string, fn func(*RuntimeRecoveryFinalizationGuard) error) error {
	if m == nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binding != binding || m.authorizationHash != authorizationHash {
		return ErrRuntimeMutationBlocked
	}
	g := &RuntimeRecoveryFinalizationGuard{state: &runtimeRecoveryFinalizationGuardState{owner: m}}
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

const runtimeRecoveryFinalSubmissionFile = "recovery-final-submission.json"
const runtimeRecoveryFinalAcknowledgementFile = "recovery-final-acknowledgement.json"

func runtimeRecoveryFinalStateAbsent(dir string) bool {
	if !preparedReleaseStateAbsent(dir) || !runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryReleaseIntentFile)) {
		return false
	}
	return runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryFinalConsumptionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryFinalSubmissionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryFinalAcknowledgementFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryRetirementFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryTerminalFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryTerminalSubmissionFile)) && runtimeMarkerAbsent(filepath.Join(dir, runtimeRecoveryTerminalAcknowledgementFile))
}
func (m *RuntimeRecoveryFinalization) finalSubmissionUnchanged() bool {
	for _, state := range []struct {
		name string
		data []byte
	}{{runtimeRecoveryFinalSubmissionFile, m.submission}, {runtimeRecoveryFinalAcknowledgementFile, m.acknowledgement}, {runtimeRecoveryRetirementFile, m.retirement}, {runtimeRecoveryTerminalFile, m.terminal}, {runtimeRecoveryTerminalSubmissionFile, m.terminalSubmission}, {runtimeRecoveryTerminalAcknowledgementFile, m.terminalAcknowledgement}, {runtimeRecoveryReleaseIntentFile, m.releaseIntent}, {runtimeRecoveryPreparedSubmissionFile, m.preparedSubmission}, {runtimeRecoveryPreparedAcknowledgementFile, m.preparedAcknowledgement}} {
		path := filepath.Join(m.dir, state.name)
		if state.data == nil {
			if !runtimeMarkerAbsent(path) {
				return false
			}
			continue
		}
		data, err := readRemovalFile(path, 8192, true)
		if err != nil || !bytes.Equal(data, state.data) {
			return false
		}
	}
	return true
}
