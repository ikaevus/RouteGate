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

const runtimeRecoveryConsumptionFile = "recovery-consumption.json"
const runtimeRecoveryConsumptionLimit = 16384
const runtimeRecoveryGrantPurpose = "runtime_mutation_fence_release_v1"

// Mirrors Manager's versioned grant contract, pinned by the shared golden
// fixture. No Agent commit dispatch or fence-release capability exists yet.
type RuntimeRecoveryMutationGrant struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	Purpose                   string                     `json:"purpose"`
	OperationID               string                     `json:"operationId"`
	Nonce                     string                     `json:"nonce"`
	AgentCredentialGeneration int64                      `json:"agentCredentialGeneration"`
	ChallengeHash             string                     `json:"challengeHash"`
	EvidenceHash              string                     `json:"evidenceHash"`
	IssuedAt                  time.Time                  `json:"issuedAt"`
	ExpiresAt                 time.Time                  `json:"expiresAt"`
	Expected                  RuntimeRecoveryObservation `json:"expected"`
}

// Binding must come from independently authenticated current credentials,
// never by copying the generation from untrusted grant data. Integration with
// credential-generation negotiation must precede any trusted caller.
type RuntimeRecoveryGrantBinding struct {
	RuntimeResultBinding
	AgentCredentialGeneration int64 `json:"agentCredentialGeneration"`
}

type runtimeRecoveryConsumption struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Binding       RuntimeRecoveryGrantBinding  `json:"binding"`
	Grant         RuntimeRecoveryMutationGrant `json:"grant"`
	GrantHash     string                       `json:"grantHash"`
	ObservedAt    time.Time                    `json:"observedAt"`
	Observation   RuntimeRecoveryObservation   `json:"observation"`
}

// Proof is a measured snapshot from a live lock owner, not permission to
// unlock. No nonce, credential, config, command output or local path is exposed.
type RuntimeRecoveryCommitProof struct {
	SchemaVersion   int                        `json:"schemaVersion"`
	OperationID     string                     `json:"operationId"`
	GrantHash       string                     `json:"grantHash"`
	ConsumptionHash string                     `json:"consumptionHash"`
	ObservedAt      time.Time                  `json:"observedAt"`
	Observation     RuntimeRecoveryObservation `json:"observation"`
}

// RuntimeRecoveryCommit keeps the admission lock across observation and local
// consumption. There is deliberately no Complete, Unlock or cleanup method.
// Close drops only the process lock; consumption, receipt and fence survive.
type RuntimeRecoveryCommit struct {
	mu              sync.Mutex
	lock            *os.File
	dir, activePath string
	binding         RuntimeRecoveryGrantBinding
	grant           RuntimeRecoveryMutationGrant
	grantHash       string
	record          []byte
	adapter         VPNCoreAdapter
	now             func() time.Time
	lastChecked     time.Time
	poisoned        bool
	submission      []byte
	frozen          RuntimeRecoveryCommitProof
}

func decodeRuntimeRecoveryMutationGrant(data []byte) (RuntimeRecoveryMutationGrant, error) {
	var g RuntimeRecoveryMutationGrant
	if len(data) > 8192 {
		return g, ErrRuntimeMutationBlocked
	}
	_, canonical, _, err := canonicalRemovalJSON(data)
	if err != nil {
		return g, ErrRuntimeMutationBlocked
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&g) != nil {
		return RuntimeRecoveryMutationGrant{}, ErrRuntimeMutationBlocked
	}
	wire, err := json.Marshal(g)
	if err != nil {
		return RuntimeRecoveryMutationGrant{}, ErrRuntimeMutationBlocked
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || g.SchemaVersion != 1 || g.Purpose != runtimeRecoveryGrantPurpose || !canonicalTaskIDPattern.MatchString(g.OperationID) || !validRemovalHash(g.Nonce) || !validRemovalHash(g.ChallengeHash) || !validRemovalHash(g.EvidenceHash) || g.AgentCredentialGeneration < 1 || g.IssuedAt.IsZero() || !g.ExpiresAt.After(g.IssuedAt) || g.ExpiresAt.Sub(g.IssuedAt) > runtimeRecoveryChallengeLifetime {
		return RuntimeRecoveryMutationGrant{}, ErrRuntimeMutationBlocked
	}
	o := g.Expected
	if o.SchemaVersion != 1 || !canonicalTaskIDPattern.MatchString(o.TaskID) || !canonicalTaskIDPattern.MatchString(o.AgentID) || !canonicalTaskIDPattern.MatchString(o.ServerID) || !validRemovalHash(o.MarkerHash) || !validRemovalHash(o.ReceiptHash) || !validRemovalHash(o.EnvelopeHash) || !validRuntimeWitness(o.Witness) {
		return RuntimeRecoveryMutationGrant{}, ErrRuntimeMutationBlocked
	}
	return g, nil
}

func (g RuntimeRecoveryMutationGrant) validBinding(b RuntimeRecoveryGrantBinding, now time.Time) bool {
	return b.ManagerURL != "" && len(b.ManagerURL) <= 2048 && b.AgentCredentialGeneration > 0 && g.AgentCredentialGeneration == b.AgentCredentialGeneration && g.Expected.AgentID == b.AgentID && g.Expected.ServerID == b.ServerID && !now.Before(g.IssuedAt) && now.Before(g.ExpiresAt)
}

// ValidateRuntimeRecoveryGrant validates transport data without touching local
// state. The binding must be independently authenticated before this call.
func ValidateRuntimeRecoveryGrant(data []byte, binding RuntimeRecoveryGrantBinding, now time.Time) (RuntimeRecoveryMutationGrant, string, error) {
	g, err := decodeRuntimeRecoveryMutationGrant(data)
	if err != nil || !g.validBinding(binding, now.UTC()) {
		return RuntimeRecoveryMutationGrant{}, "", ErrRuntimeMutationBlocked
	}
	wire, err := json.Marshal(g)
	if err != nil {
		return RuntimeRecoveryMutationGrant{}, "", ErrRuntimeMutationBlocked
	}
	return g, markerDigest(wire), nil
}

// BeginRuntimeRecoveryCommit is dormant: it does not authenticate/deliver a
// grant or contact Manager. Trusted callers must negotiate current generation
// independently and defer Close immediately. Local consumption is durable but
// Manager commit/reconciliation and crash-safe fence release remain absent.
func BeginRuntimeRecoveryCommit(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, data []byte, activePath string, adapter VPNCoreAdapter) (*RuntimeRecoveryCommit, error) {
	return beginRuntimeRecoveryCommit(ctx, dir, binding, data, activePath, adapter, time.Now)
}

func beginRuntimeRecoveryCommit(ctx context.Context, dir string, binding RuntimeRecoveryGrantBinding, data []byte, activePath string, adapter VPNCoreAdapter, now func() time.Time) (*RuntimeRecoveryCommit, error) {
	g, err := decodeRuntimeRecoveryMutationGrant(data)
	started := now().UTC()
	if err != nil || ctx.Err() != nil || !g.validBinding(binding, started) || !RuntimeMutationStatePresent(dir) {
		return nil, ErrRuntimeMutationBlocked
	}
	lock, err := lockRuntimeMutation(dir)
	if err != nil {
		return nil, ErrRuntimeMutationBlocked
	}
	m := &RuntimeRecoveryCommit{lock: lock, dir: dir, activePath: activePath, binding: binding, grant: g, adapter: adapter, now: now, lastChecked: started}
	fail := func() (*RuntimeRecoveryCommit, error) { m.Close(); return nil, ErrRuntimeMutationBlocked }
	if !runtimeRecoveryStateAbsent(dir) {
		return fail()
	}
	bounded, cancel := context.WithDeadline(ctx, g.ExpiresAt)
	defer cancel()
	o, err := observeRuntimeRecoveryLocked(bounded, dir, binding.RuntimeResultBinding, g.Expected.TaskID, activePath, adapter, lock)
	finished := now().UTC()
	if err != nil || bounded.Err() != nil || finished.Before(started) || !g.validBinding(binding, finished) || o != g.Expected {
		return fail()
	}
	wire, err := json.Marshal(g)
	if err != nil {
		return fail()
	}
	m.grantHash = markerDigest(wire)
	m.record, err = json.Marshal(runtimeRecoveryConsumption{1, binding, g, m.grantHash, finished, o})
	if err != nil || len(m.record) > runtimeRecoveryConsumptionLimit {
		return fail()
	}
	// Atomic rename and fsync are complete before any proof can be returned.
	// Errors after publication retain the record and the original intent.
	if writeRemovalFile(filepath.Join(dir, runtimeRecoveryConsumptionFile), m.record) != nil {
		return fail()
	}
	m.lastChecked = finished
	if _, err = m.Proof(ctx); err != nil {
		return fail()
	}
	return m, nil
}

func (m *RuntimeRecoveryCommit) Proof(ctx context.Context) (RuntimeRecoveryCommitProof, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.proofLocked(ctx)
}

func (m *RuntimeRecoveryCommit) proofLocked(ctx context.Context) (RuntimeRecoveryCommitProof, error) {
	deny := func() (RuntimeRecoveryCommitProof, error) {
		m.poisoned = true
		return RuntimeRecoveryCommitProof{}, ErrRuntimeMutationBlocked
	}
	if m.lock == nil || m.poisoned || m.now == nil || ctx.Err() != nil || !m.submissionUnchanged() {
		return deny()
	}
	started := m.now().UTC()
	if started.Before(m.lastChecked) || !m.grant.validBinding(m.binding, started) {
		return deny()
	}
	path := filepath.Join(m.dir, runtimeRecoveryConsumptionFile)
	before, err := readRemovalFile(path, runtimeRecoveryConsumptionLimit, true)
	if err != nil || !bytes.Equal(before, m.record) {
		return deny()
	}
	bounded, cancel := context.WithDeadline(ctx, m.grant.ExpiresAt)
	defer cancel()
	o, err := observeRuntimeRecoveryLocked(bounded, m.dir, m.binding.RuntimeResultBinding, m.grant.Expected.TaskID, m.activePath, m.adapter, m.lock)
	after, fileErr := readRemovalFile(path, runtimeRecoveryConsumptionLimit, true)
	finished := m.now().UTC()
	if err != nil || fileErr != nil || bounded.Err() != nil || !bytes.Equal(after, m.record) || finished.Before(started) || !m.grant.validBinding(m.binding, finished) || o != m.grant.Expected || !m.submissionUnchanged() {
		return deny()
	}
	m.lastChecked = finished
	return RuntimeRecoveryCommitProof{1, m.grant.OperationID, m.grantHash, markerDigest(m.record), finished, o}, nil
}

func (m *RuntimeRecoveryCommit) Close() error {
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
