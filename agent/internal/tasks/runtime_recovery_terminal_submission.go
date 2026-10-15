package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const runtimeRecoveryTerminalSubmissionFile = "recovery-terminal-submission.json"
const runtimeRecoveryTerminalAcknowledgementFile = "recovery-terminal-acknowledgement.json"
const runtimeRecoveryTerminalEvidencePurpose = "runtime_mutation_retirement_evidence_v1"

// Mirrors Manager archival evidence, never authority to release a fence.
type RuntimeRecoveryTerminalProof struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	AuthorizationHash         string                     `json:"authorizationHash"`
	FinalConsumptionHash      string                     `json:"finalConsumptionHash"`
	FinalProofHash            string                     `json:"finalProofHash"`
	FinalReceiptHash          string                     `json:"finalReceiptHash"`
	IntentHash                string                     `json:"intentHash"`
	TerminalHash              string                     `json:"terminalHash"`
	ObservedAt                time.Time                  `json:"observedAt"`
	Observation               RuntimeRecoveryObservation `json:"observation"`
}
type RuntimeRecoveryTerminalReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recorded      bool   `json:"recorded"`
	OperationID   string `json:"operationId"`
	TerminalHash  string `json:"terminalHash"`
	ProofHash     string `json:"proofHash"`
	RequestHash   string `json:"requestHash"`
}

func terminalProofProjection(data []byte, p RuntimeRecoveryFinalizationProof, b RuntimeRecoveryGrantBinding) (RuntimeRecoveryTerminalProof, error) {
	var r runtimeRecoveryTerminalRecord
	if !exactHistoricalJSON(data, &r) || r.SchemaVersion != 1 || r.Purpose != runtimeRecoveryTerminalPurpose || r.Binding != b || r.OperationID != p.OperationID || r.Observation != p.Observation || r.ObservedAt.Before(p.ObservedAt) {
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	return RuntimeRecoveryTerminalProof{1, runtimeRecoveryTerminalEvidencePurpose, p.OperationID, b.AgentCredentialGeneration, p.AuthorizationHash, p.FinalConsumptionHash, r.FinalProofHash, r.FinalReceiptHash, r.IntentHash, markerDigest(data), r.ObservedAt, p.Observation}, nil
}
func terminalReceiptMatches(r RuntimeRecoveryTerminalReceipt, p RuntimeRecoveryTerminalProof, wire []byte) bool {
	return r.SchemaVersion == 1 && r.Recorded && r.OperationID == p.OperationID && r.TerminalHash == p.TerminalHash && r.ProofHash == markerDigest(wire) && r.RequestHash == r.ProofHash
}

func readTerminalConfirmationFile(path string, limit int64) ([]byte, error) {
	private := func(info os.FileInfo) bool {
		stat, ok := info.Sys().(*syscall.Stat_t)
		return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
	}
	before, e := os.Lstat(path)
	if e != nil || !private(before) {
		return nil, ErrRuntimeMutationBlocked
	}
	data, e := readRemovalFile(path, limit, true)
	if e != nil {
		return nil, ErrRuntimeMutationBlocked
	}
	after, e := os.Lstat(path)
	if e != nil || !private(after) || !os.SameFile(before, after) {
		return nil, ErrRuntimeMutationBlocked
	}
	return data, nil
}

// Validate optional new-phase files as exact retained history. A historical
// reader never writes/adopts them or gets fresh live authority from their bytes.
func terminalConfirmationSnapshot(dir string, terminal []byte, p RuntimeRecoveryFinalizationProof, b RuntimeRecoveryGrantBinding) (func() bool, error) {
	proof, e := terminalProofProjection(terminal, p, b)
	if e != nil {
		return nil, e
	}
	wire, e := json.Marshal(proof)
	if e != nil {
		return nil, ErrRuntimeMutationBlocked
	}
	pinPath, ackPath := filepath.Join(dir, runtimeRecoveryTerminalSubmissionFile), filepath.Join(dir, runtimeRecoveryTerminalAcknowledgementFile)
	var pin, ack []byte
	if !runtimeMarkerAbsent(pinPath) {
		pin, e = readTerminalConfirmationFile(pinPath, 8192)
		if e != nil || !bytes.Equal(pin, wire) {
			return nil, ErrRuntimeMutationBlocked
		}
	}
	if !runtimeMarkerAbsent(ackPath) {
		if pin == nil {
			return nil, ErrRuntimeMutationBlocked
		}
		ack, e = readTerminalConfirmationFile(ackPath, 4096)
		var r RuntimeRecoveryTerminalReceipt
		if e != nil || !exactHistoricalJSON(ack, &r) || !terminalReceiptMatches(r, proof, pin) {
			return nil, ErrRuntimeMutationBlocked
		}
	}
	unchanged := func() bool {
		for _, state := range []struct {
			path string
			data []byte
		}{{pinPath, pin}, {ackPath, ack}} {
			if state.data == nil {
				if !runtimeMarkerAbsent(state.path) {
					return false
				}
				continue
			}
			data, e := readTerminalConfirmationFile(state.path, 8192)
			if e != nil || !bytes.Equal(data, state.data) {
				return false
			}
		}
		return true
	}
	return unchanged, nil
}
func (m *RuntimeRecoveryFinalization) terminalSubmissionUnchanged() bool {
	for _, state := range []struct {
		name string
		data []byte
	}{{runtimeRecoveryTerminalSubmissionFile, m.terminalSubmission}, {runtimeRecoveryTerminalAcknowledgementFile, m.terminalAcknowledgement}} {
		path := filepath.Join(m.dir, state.name)
		if state.data == nil {
			if !runtimeMarkerAbsent(path) {
				return false
			}
			continue
		}
		data, e := readTerminalConfirmationFile(path, 8192)
		if e != nil || !bytes.Equal(data, state.data) {
			return false
		}
	}
	return true
}
func (m *RuntimeRecoveryFinalization) terminalProofLocked(ctx context.Context) (RuntimeRecoveryTerminalProof, error) {
	deny := func() (RuntimeRecoveryTerminalProof, error) {
		m.poisoned = true
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	if m.terminal == nil {
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	if _, e := m.terminalObservation(ctx); e != nil || !m.terminalSubmissionUnchanged() {
		return deny()
	}
	p, e := terminalProofProjection(m.terminal, m.frozen, m.binding)
	if e != nil {
		return deny()
	}
	if m.terminalSubmission != nil && p != m.terminalFrozen {
		return deny()
	}
	return p, nil
}

type RuntimeRecoveryTerminalGuard struct {
	state *runtimeRecoveryTerminalGuardState
}
type runtimeRecoveryTerminalGuardState struct {
	mu    sync.Mutex
	owner *RuntimeRecoveryFinalization
}

func (g *RuntimeRecoveryTerminalGuard) Proof(ctx context.Context) (RuntimeRecoveryTerminalProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	return g.state.owner.terminalProofLocked(ctx)
}
func (g *RuntimeRecoveryTerminalGuard) PinProof(ctx context.Context) (RuntimeRecoveryTerminalProof, error) {
	if g == nil || g.state == nil {
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	p, e := m.terminalProofLocked(ctx)
	if e != nil {
		return RuntimeRecoveryTerminalProof{}, e
	}
	if m.terminalSubmission != nil {
		return m.terminalFrozen, nil
	}
	data, e := json.Marshal(p)
	if e != nil || len(data) > 8192 || !m.terminalAuthorityCurrent(ctx) || writeRemovalFile(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), data) != nil {
		m.poisoned = true
		return RuntimeRecoveryTerminalProof{}, ErrRuntimeMutationBlocked
	}
	m.terminalSubmission = data
	m.terminalFrozen = p
	if _, e = m.terminalProofLocked(ctx); e != nil {
		return RuntimeRecoveryTerminalProof{}, e
	}
	return p, nil
}
func (g *RuntimeRecoveryTerminalGuard) RecordReceipt(ctx context.Context, r RuntimeRecoveryTerminalReceipt) error {
	if g == nil || g.state == nil {
		return ErrRuntimeMutationBlocked
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.owner == nil {
		return ErrRuntimeMutationBlocked
	}
	m := g.state.owner
	if _, e := m.terminalProofLocked(ctx); e != nil {
		return e
	}
	if m.terminalSubmission == nil || !terminalReceiptMatches(r, m.terminalFrozen, m.terminalSubmission) {
		return ErrRuntimeMutationBlocked
	}
	data, e := json.Marshal(r)
	if e != nil || len(data) > 4096 {
		return ErrRuntimeMutationBlocked
	}
	if m.terminalAcknowledgement != nil {
		if !bytes.Equal(data, m.terminalAcknowledgement) {
			return ErrRuntimeMutationBlocked
		}
		return nil
	}
	if !m.terminalAuthorityCurrent(ctx) || writeRemovalFile(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile), data) != nil {
		m.poisoned = true
		return ErrRuntimeMutationBlocked
	}
	m.terminalAcknowledgement = data
	_, e = m.terminalProofLocked(ctx)
	return e
}

// Original still-live owner only. Synchronous callback holds the same mutex/
// root flock through HTTP, receipt durability and fresh final observation.
// Escaped guard copies expire together. Close never removes retained state.
func (m *RuntimeRecoveryFinalization) WithTerminalSubmission(ctx context.Context, b RuntimeRecoveryGrantBinding, authorizationHash string, fn func(*RuntimeRecoveryTerminalGuard) error) error {
	if m == nil || fn == nil {
		return ErrRuntimeMutationBlocked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.binding != b || m.authorizationHash != authorizationHash || m.terminal == nil {
		return ErrRuntimeMutationBlocked
	}
	g := &RuntimeRecoveryTerminalGuard{state: &runtimeRecoveryTerminalGuardState{owner: m}}
	defer func() { g.state.mu.Lock(); g.state.owner = nil; g.state.mu.Unlock() }()
	if _, e := g.Proof(ctx); e != nil {
		return e
	}
	e := fn(g)
	if _, final := g.Proof(ctx); final != nil {
		return final
	}
	if e != nil {
		return ErrRuntimeMutationBlocked
	}
	return nil
}
