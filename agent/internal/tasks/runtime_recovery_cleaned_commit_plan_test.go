package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func acknowledgeCleanedCommitForPlan(t *testing.T, b RuntimeRecoveryGrantBinding, m *RuntimeRecoveryFinalization) {
	t.Helper()
	prepareCleanedCommit(t, b, m)
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.PinCleanedCommitProof(context.Background())
		if e != nil {
			return e
		}
		wire, _ := json.Marshal(p)
		h := markerDigest(wire)
		return g.RecordCleanedCommitReceipt(context.Background(), RuntimeRecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, h, h})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanedCommitPlanReadOnly(t *testing.T) {
	_, _, b, m, path := cleanedCommitFixture(t)
	acknowledgeCleanedCommitForPlan(t, b, m)
	before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, path)
	lock, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	var first RuntimeRecoveryCleanedCommitPlan
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	for range 2 {
		if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			p, e := g.PlanCleanedCommit(context.Background())
			if e != nil {
				return e
			}
			if p.OperationID != m.authorization.OperationID || p.Binding != b || p.AuthorizationHash != m.authorizationHash || p.ExpiresAt != m.authorization.ExpiresAt || p.ProofHash != markerDigest(m.cleanedCommitSubmission) || p.ReceiptHash != markerDigest(m.cleanedCommitReceipt) || p.ManifestHash != markerDigest(m.historyArchive) || p.AdmissionLockName != "runtime.lock" || p.HistoryArchiveName != filepath.Base(path) || p.ObservedAt.Before(m.cleanedCommitFrozen.ObservedAt) {
				t.Error("plan bindings")
			}
			for _, f := range p.Files {
				if f.Name == "runtime.lock" || f.Name == filepath.Base(path) || markerDigest(before[f.Name]) != f.Hash {
					t.Error("unsafe inventory", f.Name)
				}
			}
			if first.OperationID != "" {
				stamp := p.ObservedAt
				p.ObservedAt = first.ObservedAt
				if p != first || stamp.Before(first.ObservedAt) {
					t.Error("plan renewed authority/inventory")
				}
			} else {
				first = p
			}
			p.Files[0].Hash = "caller edit" // Value copy never enters owner state.
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	copyGuard := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &copyGuard} {
		if p, e := g.PlanCleanedCommit(context.Background()); e != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryCleanedCommitPlan{}) {
			t.Fatal("escaped plan", e)
		}
		if p, r, e := g.AcknowledgedCleanedCommit(context.Background()); e != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryCleanedCommitProof{}) || r != (RuntimeRecoveryCleanedCommitReceipt{}) {
			t.Fatal("escaped acknowledgement", e)
		}
	}
	after, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if !os.SameFile(lock, after) {
		t.Fatal("lock replaced")
	}
	m.Close()
	if lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); e != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("planner released fence")
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, backup)
}

func TestCleanedCommitPlanRefusals(t *testing.T) {
	for _, mode := range []string{"unacknowledged", "missing_proof", "missing_ack", "missing_intent", "generation", "manager", "authorization", "closed", "poisoned", "cancelled", "expired", "backward", "runtime", "proof_replaced", "ack_replaced", "barrier_replaced", "copy_replaced", "unknown", "proof_during_observation", "unknown_during_observation", "expiry_during_observation", "backward_during_observation", "expiry_at_return"} {
		t.Run(mode, func(t *testing.T) {
			e, _, b, m, path := cleanedCommitFixture(t)
			if mode == "unacknowledged" {
				prepareCleanedCommit(t, b, m)
				if e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
					_, e := g.PinCleanedCommitProof(context.Background())
					return e
				}); e != nil {
					t.Fatal(e)
				}
			} else {
				acknowledgeCleanedCommitForPlan(t, b, m)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hash := m.authorizationHash
			switch mode {
			case "missing_proof":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryCleanedCommitSubmissionFile))
			case "missing_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryCleanedCommitAcknowledgementFile))
			case "missing_intent":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile))
			case "generation":
				b.AgentCredentialGeneration++
			case "manager":
				b.ManagerURL = "https://foreign.invalid"
			case "authorization":
				hash = "foreign"
			case "closed":
				m.Close()
			case "poisoned":
				m.poisoned = true
			case "cancelled":
				cancel()
			case "expired":
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			case "backward":
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			case "runtime":
				os.WriteFile(e.activePath, []byte("{}"), 0600)
			case "proof_replaced":
				replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryCleanedCommitSubmissionFile))
			case "ack_replaced":
				replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryCleanedCommitAcknowledgementFile))
			case "barrier_replaced":
				replaceHistoryFile(t, filepath.Join(m.dir, "mutation-inflight.json"))
			case "copy_replaced":
				replaceHistoryFile(t, filepath.Join(path, "manifest.json"))
			case "unknown":
				os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
			case "proof_during_observation", "unknown_during_observation", "expiry_during_observation", "backward_during_observation", "expiry_at_return":
				calls := 0
				m.now = func() time.Time {
					calls++
					if mode == "expiry_at_return" && calls >= 10 {
						return m.authorization.ExpiresAt
					}
					if calls == 7 {
						switch mode {
						case "proof_during_observation":
							replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryCleanedCommitSubmissionFile))
						case "unknown_during_observation":
							os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
						case "expiry_during_observation":
							return m.authorization.ExpiresAt
						case "backward_during_observation":
							return m.lastChecked.Add(-time.Second)
						}
					}
					return time.Now().UTC()
				}
			}
			before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, path)
			var result RuntimeRecoveryCleanedCommitPlan
			err := m.WithSourceCleanupSubmission(ctx, b, hash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var e error
				result, e = g.PlanCleanedCommit(ctx)
				return e
			})
			if err != ErrRuntimeMutationBlocked || result != (RuntimeRecoveryCleanedCommitPlan{}) {
				t.Fatal("unsafe plan", err)
			}
			if mode != "unknown_during_observation" {
				assertTerminalHistoryTree(t, m.dir, before)
			}
			assertTerminalHistoryTree(t, path, backup)
		})
	}
}

func TestCleanedCommitPlanSerializesClose(t *testing.T) {
	_, _, b, m, _ := cleanedCommitFixture(t)
	acknowledgeCleanedCommitForPlan(t, b, m)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.now = func() time.Time { once.Do(func() { close(entered); <-resume }); return time.Now().UTC() }
	planned, closed := make(chan error, 1), make(chan struct{})
	go func() {
		planned <- m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			_, e := g.PlanCleanedCommit(context.Background())
			return e
		})
	}()
	<-entered
	go func() { m.Close(); close(closed) }()
	if lock, e := lockRuntimeMutationExisting(m.dir); e == nil {
		lock.Close()
		t.Error("planning dropped flock")
	}
	select {
	case <-closed:
		t.Error("Close bypassed mutex")
	default:
	}
	close(resume)
	if err := <-planned; err != nil {
		t.Fatal(err)
	}
	<-closed
}
