package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const runtimeRecoveryFinalConsumptionFile = "recovery-final-consumption.json"

type runtimeRecoveryFinalConsumption struct {
	SchemaVersion     int                                 `json:"schemaVersion"`
	Binding           RuntimeRecoveryGrantBinding         `json:"binding"`
	Authorization     RuntimeRecoveryReleaseAuthorization `json:"authorization"`
	AuthorizationHash string                              `json:"authorizationHash"`
	ObservedAt        time.Time                           `json:"observedAt"`
	Observation       RuntimeRecoveryObservation          `json:"observation"`
}
type RuntimeRecoveryFinalizationProof struct {
	SchemaVersion        int                        `json:"schemaVersion"`
	OperationID          string                     `json:"operationId"`
	AuthorizationHash    string                     `json:"authorizationHash"`
	FinalConsumptionHash string                     `json:"finalConsumptionHash"`
	ObservedAt           time.Time                  `json:"observedAt"`
	Observation          RuntimeRecoveryObservation `json:"observation"`
}

// Dormant exclusive owner. There is no Complete/Release/resume method. Close
// drops only flock; all private history, final consumption and fences survive.
type RuntimeRecoveryFinalization struct {
	mu                sync.Mutex
	lock              *os.File
	dir, activePath   string
	adapter           VPNCoreAdapter
	binding           RuntimeRecoveryGrantBinding
	authorization     RuntimeRecoveryReleaseAuthorization
	authorizationHash string
	pinned            RuntimeRecoveryCommitProof
	unchanged         func() bool
	record            []byte
	submission        []byte
	frozen            RuntimeRecoveryFinalizationProof
	acknowledgement   []byte
	retirement        []byte
	now               func() time.Time
	lastChecked       time.Time
	poisoned          bool
}

// Binding and authorization must come from independently authenticated current
// Manager delivery. History is never authority and generation is not copied
// from authorization. Only fresh locked observations permit local consumption.
func BeginRuntimeRecoveryFinalization(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, data []byte, activePath string, adapter VPNCoreAdapter) (*RuntimeRecoveryFinalization, error) {
	return beginRuntimeRecoveryFinalization(ctx, dir, binding, data, activePath, adapter, time.Now)
}
func beginRuntimeRecoveryFinalization(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, data []byte, activePath string, adapter VPNCoreAdapter, now func() time.Time) (*RuntimeRecoveryFinalization, error) {
	if ctx.Err() != nil || now == nil {
		return nil, ErrRuntimeMutationBlocked
	}
	started := now().UTC()
	lock, err := lockRuntimeMutationExisting(dir)
	if err != nil {
		return nil, ErrRuntimeMutationBlocked
	}
	m := &RuntimeRecoveryFinalization{lock: lock, dir: dir, activePath: activePath, adapter: adapter, binding: binding, now: now, lastChecked: started}
	fail := func() (*RuntimeRecoveryFinalization, error) { m.Close(); return nil, ErrRuntimeMutationBlocked }
	if !runtimeRecoveryFinalStateAbsent(dir) {
		return fail()
	}
	err = withHistoricalRecoverySubmissionLocked(ctx, dir, binding, lock, func(p RuntimeRecoveryCommitProof, unchanged func() bool) error {
		a, hash, e := ValidateRuntimeRecoveryReleaseAuthorization(data, binding, p, now().UTC())
		if e != nil {
			return e
		}
		// Final nonce must be distinct even when supplied outside transport.
		old, e := readRemovalFile(filepath.Join(dir, runtimeRecoveryConsumptionFile), runtimeRecoveryConsumptionLimit, true)
		var original runtimeRecoveryConsumption
		if e != nil || !exactHistoricalJSON(old, &original) || a.Nonce == original.Grant.Nonce {
			return ErrRuntimeMutationBlocked
		}
		m.authorization = a
		m.authorizationHash = hash
		m.pinned = p
		m.unchanged = unchanged
		proof, e := m.Proof(ctx)
		if e != nil {
			return e
		}
		m.record, e = json.Marshal(runtimeRecoveryFinalConsumption{1, binding, a, hash, proof.ObservedAt, proof.Observation})
		if e != nil || len(m.record) > runtimeRecoveryConsumptionLimit {
			return ErrRuntimeMutationBlocked
		}
		if writeRemovalFile(filepath.Join(dir, runtimeRecoveryFinalConsumptionFile), m.record) != nil {
			m.poisoned = true
			return ErrRuntimeMutationBlocked
		}
		_, e = m.Proof(ctx)
		return e
	})
	if err != nil {
		return fail()
	}
	return m, nil
}

func (m *RuntimeRecoveryFinalization) Proof(ctx context.Context) (RuntimeRecoveryFinalizationProof, error) {
	if m == nil {
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.proofLocked(ctx)
}
func (m *RuntimeRecoveryFinalization) proofLocked(ctx context.Context) (RuntimeRecoveryFinalizationProof, error) {
	deny := func() (RuntimeRecoveryFinalizationProof, error) {
		m.poisoned = true
		return RuntimeRecoveryFinalizationProof{}, ErrRuntimeMutationBlocked
	}
	if m.lock == nil || m.poisoned || m.now == nil || m.unchanged == nil || ctx.Err() != nil || !m.unchanged() || !m.finalSubmissionUnchanged() {
		return deny()
	}
	file := filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile)
	recordUnchanged := func() bool {
		if m.record == nil {
			return runtimeMarkerAbsent(file)
		}
		data, e := readRemovalFile(file, runtimeRecoveryConsumptionLimit, true)
		return e == nil && bytes.Equal(data, m.record)
	}
	started := m.now().UTC()
	a := m.authorization
	wire, _ := json.Marshal(a)
	if started.Before(m.lastChecked) || !recordUnchanged() {
		return deny()
	}
	if _, hash, e := ValidateRuntimeRecoveryReleaseAuthorization(wire, m.binding, m.pinned, started); e != nil || hash != m.authorizationHash {
		return deny()
	}
	bounded, cancel := context.WithDeadline(ctx, a.ExpiresAt)
	defer cancel()
	o, err := observeRuntimeRecoveryLocked(bounded, m.dir, m.binding.RuntimeResultBinding, a.Expected.TaskID, m.activePath, m.adapter, m.lock)
	finished := m.now().UTC()
	if err != nil || bounded.Err() != nil || finished.Before(started) || finished.Before(a.IssuedAt) || !finished.Before(a.ExpiresAt) || o != a.Expected || !m.unchanged() || !recordUnchanged() || !m.finalSubmissionUnchanged() {
		return deny()
	}
	m.lastChecked = finished
	// Before consumption, this proof is internal only and its digest is empty.
	digest := ""
	if m.record != nil {
		digest = markerDigest(m.record)
	}
	return RuntimeRecoveryFinalizationProof{1, a.OperationID, m.authorizationHash, digest, finished, o}, nil
}

func (m *RuntimeRecoveryFinalization) Close() error {
	if m == nil {
		return ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lock == nil {
		return nil
	}
	err := m.lock.Close()
	m.lock = nil
	if err != nil {
		return ErrRuntimeMutationBlocked
	}
	return nil
}
