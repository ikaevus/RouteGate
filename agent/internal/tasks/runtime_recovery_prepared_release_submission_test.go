package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func preparedSubmissionFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryReleaseAuthorization, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, auth, m := releaseIntentFixture(t)
	if _, err := m.PrepareFenceRelease(context.Background(), b, m.authorizationHash); err != nil {
		m.Close()
		t.Fatal(err)
	}
	return e, a, b, auth, m
}

func TestPreparedReleaseOrphanLegacyGuards(t *testing.T) {
	for _, name := range []string{runtimeRecoveryPreparedSubmissionFile, runtimeRecoveryPreparedAcknowledgementFile, runtimeRecoveryReleaseRetentionFile} {
		for _, data := range []string{"", "{}", "malformed"} {
			t.Run(data, func(t *testing.T) {
				e, task, a, _ := removalExecutorFixture(t)
				lease, err := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService)
				if err != nil {
					t.Fatal(err)
				}
				if e := os.WriteFile(filepath.Join(e.receiptDir, name), []byte(data), 0600); e != nil {
					t.Fatal(e)
				}
				if lease.Complete() != ErrRuntimeMutationBlocked {
					t.Fatal("intent alone allowed completion")
				}
				if lease.SaveResult(RuntimeResultBinding{}, task.ID, []byte(`{"status":"succeeded"}`)) != ErrRuntimeMutationBlocked {
					t.Fatal("intent alone allowed result save")
				}
				lease.Close()
				if e := os.Remove(filepath.Join(e.receiptDir, "mutation-inflight.json")); e != nil {
					t.Fatal(e)
				}
				if l, e := BeginRuntimeMutation(e.receiptDir, task.ID, TaskKindVPNCoreService); e == nil {
					l.Close()
					t.Fatal("intent alone allowed admission")
				}
				if _, e := e.Execute(context.Background(), task); e == nil || a.restarts != 0 {
					t.Fatal("intent alone allowed removal")
				}
				if e := ReplayRuntimeResult(e.receiptDir, RuntimeResultBinding{}, func(string, []byte) error { t.Error("unexpected delivery"); return nil }); e == nil {
					t.Fatal("intent alone allowed replay")
				}
			})
		}
	}

}
func receiptForPreparedProof(p RuntimeRecoveryPreparedReleaseProof) RuntimeRecoveryPreparedReleaseReceipt {
	wire, _ := json.Marshal(p)
	hash := markerDigest(wire)
	return RuntimeRecoveryPreparedReleaseReceipt{1, true, p.OperationID, p.ReleaseIntentHash, hash, hash}
}
func TestPreparedReleaseDurability(t *testing.T) {
	_, _, b, auth, m := preparedSubmissionFixture(t)
	defer m.Close()
	before := terminalHistoryTree(t, m.dir)
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	var copied RuntimeRecoveryPreparedReleaseGuard
	var frozen RuntimeRecoveryPreparedReleaseProof
	for range 2 {
		if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			copied = *g
			p, err := g.PinProof(context.Background())
			if err != nil {
				return err
			}
			if frozen.SchemaVersion != 0 && frozen != p {
				t.Fatal("retry renewed proof")
			}
			frozen = p
			if l, err := lockRuntimeMutation(m.dir); err == nil {
				l.Close()
				t.Fatal("submission dropped flock")
			}
			if err = g.RecordReceipt(context.Background(), receiptForPreparedProof(p)); err != nil {
				return err
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{escaped, &copied} {
		if _, err := g.Proof(context.Background()); err != ErrRuntimeMutationBlocked {
			t.Fatal("escaped guard")
		}
		if _, err := g.PinProof(context.Background()); err != ErrRuntimeMutationBlocked {
			t.Fatal("escaped pin")
		}
		if err := g.RecordReceipt(context.Background(), receiptForPreparedProof(frozen)); err != ErrRuntimeMutationBlocked {
			t.Fatal("escaped receipt")
		}
	}
	pin, _ := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile))
	ack, _ := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile))
	var p RuntimeRecoveryPreparedReleaseProof
	var r RuntimeRecoveryPreparedReleaseReceipt
	if !exactHistoricalJSON(pin, &p) || p != frozen || !exactHistoricalJSON(ack, &r) || !preparedReleaseReceiptMatches(r, p, pin) || p.ReleaseIntentHash != markerDigest(m.releaseIntent) || p.TerminalProofHash != markerDigest(m.terminalSubmission) || p.TerminalReceiptHash != markerDigest(m.terminalAcknowledgement) || p.ObservedAt.Before(p.PreparedAt) || !p.ObservedAt.Before(auth.ExpiresAt) {
		t.Fatal("durable bindings")
	}
	after := terminalHistoryTree(t, m.dir)
	delete(after, runtimeRecoveryPreparedSubmissionFile)
	delete(after, runtimeRecoveryPreparedAcknowledgementFile)
	if len(before) != len(after) {
		t.Fatal("changed retained set")
	}
	for k, v := range before {
		if !bytes.Equal(v, after[k]) {
			t.Fatal("changed retained history", k)
		}
	}
	m.Close()
	all := terminalHistoryTree(t, m.dir)
	if err := WithHistoricalRecoveryPreparedRelease(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedReleaseHistory) error {
		t.Error("submission adopted by old reader")
		return nil
	}); err != ErrRuntimeMutationBlocked {
		t.Fatal(err)
	}
	if l, err := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if l != nil {
			l.Close()
		}
		t.Fatal("released fence")
	}
	assertTerminalHistoryTree(t, m.dir, all)
}

func TestPreparedReleaseRefusals(t *testing.T) {
	for _, mode := range []string{"no_intent", "binding", "authorization", "expired", "backwards_clock", "runtime", "intent", "foreign_pin", "identical_foreign_pin", "foreign_ack", "cancelled", "callback_error", "drift_after_pin", "expiry_after_pin", "cancel_after_pin", "same_byte_pin", "same_byte_intent", "unsafe_pin", "ack_without_pin", "pin_postwrite_cancel", "ack_postwrite_cancel"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, auth, m := preparedSubmissionFixture(t)
			defer m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hash := m.authorizationHash
			switch mode {
			case "no_intent":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile))
				m.releaseIntent = nil
			case "binding":
				b.AgentCredentialGeneration++
			case "authorization":
				hash = strings.Repeat("e", 64)
			case "expired":
				m.now = func() time.Time { return auth.ExpiresAt }
			case "backwards_clock":
				m.now = func() time.Time { return m.lastChecked.Add(-time.Nanosecond) }
			case "runtime":
				a.generation.InvocationID = "foreign"
			case "intent":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile), []byte("{}"), 0600)
			case "foreign_pin":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile), []byte("{}"), 0600)
			case "identical_foreign_pin":
				p, e := m.preparedReleaseProofLocked(ctx)
				if e != nil {
					t.Fatal(e)
				}
				wire, _ := json.Marshal(p)
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile), wire, 0600)
			case "foreign_ack", "ack_without_pin":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile), []byte("{}"), 0600)
			case "cancelled":
				cancel()
			}
			err := m.WithPreparedReleaseSubmission(ctx, b, hash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				p, e := g.pinProof(ctx, func() error {
					if mode == "pin_postwrite_cancel" {
						cancel()
					}
					return nil
				})
				if e != nil {
					return e
				}
				switch mode {
				case "callback_error":
					return errors.New("secret payload")
				case "drift_after_pin":
					a.generation.InvocationID = "foreign"
				case "expiry_after_pin":
					m.now = func() time.Time { return auth.ExpiresAt }
				case "cancel_after_pin":
					cancel()
				case "same_byte_pin", "same_byte_intent":
					name := runtimeRecoveryPreparedSubmissionFile
					if mode == "same_byte_intent" {
						name = runtimeRecoveryReleaseIntentFile
					}
					path := filepath.Join(m.dir, name)
					data, _ := os.ReadFile(path)
					tmp := filepath.Join(m.dir, "replacement")
					if os.WriteFile(tmp, data, 0600) != nil || os.Rename(tmp, path) != nil {
						t.Fatal("replacement")
					}
				case "unsafe_pin":
					os.Chmod(filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile), 0644)
				}
				return g.recordReceipt(ctx, receiptForPreparedProof(p), func() error {
					if mode == "ack_postwrite_cancel" {
						cancel()
					}
					return nil
				})
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe submission", err)
			}
			if mode != "no_intent" && runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)) {
				t.Fatal("lost intent")
			}
			if runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) || privateRetirementArchive(m.archive) != nil {
				t.Fatal("lost barriers/archive")
			}
		})
	}
}

func TestPreparedReleaseReceiptBindings(t *testing.T) {
	_, _, b, _, m := preparedSubmissionFixture(t)
	defer m.Close()
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.PinProof(context.Background())
		if e != nil {
			return e
		}
		valid := receiptForPreparedProof(p)
		for _, mode := range []string{"schema", "recorded", "operation", "intent", "proof", "request"} {
			r := valid
			switch mode {
			case "schema":
				r.SchemaVersion++
			case "recorded":
				r.Recorded = false
			case "operation":
				r.OperationID = b.AgentID
			case "intent":
				r.ReleaseIntentHash = strings.Repeat("e", 64)
			case "proof":
				r.ProofHash = strings.Repeat("e", 64)
			case "request":
				r.RequestHash = strings.Repeat("e", 64)
			}
			if g.RecordReceipt(context.Background(), r) != ErrRuntimeMutationBlocked {
				t.Fatal("foreign receipt", mode)
			}
			if !runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile)) {
				t.Fatal("manufactured receipt")
			}
		}
		return g.RecordReceipt(context.Background(), valid)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedReleaseConcurrentRetryAndClose(t *testing.T) {
	_, _, b, _, m := preparedSubmissionFixture(t)
	defer m.Close()
	results := make(chan RuntimeRecoveryPreparedReleaseProof, 3)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				results <- p
				return g.RecordReceipt(context.Background(), receiptForPreparedProof(p))
			})
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	close(results)
	var first RuntimeRecoveryPreparedReleaseProof
	for p := range results {
		if first.SchemaVersion != 0 && p != first {
			t.Fatal("concurrent proof renewal")
		}
		first = p
	}
	entered, resume, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			close(entered)
			<-resume
			_, e := g.Proof(context.Background())
			return e
		})
	}()
	<-entered
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close bypassed owner mutex")
	default:
	}
	close(resume)
	if e := <-finished; e != nil {
		t.Fatal(e)
	}
	<-closed
}

func TestPreparedReleaseOrphanConfirmationBlocks(t *testing.T) {
	for _, name := range []string{runtimeRecoveryPreparedSubmissionFile, runtimeRecoveryPreparedAcknowledgementFile} {
		for _, data := range []string{"", "{}"} {
			t.Run(name+data, func(t *testing.T) {
				e, a, b, g, wire := recoveryCommitFixture(t)
				if err := os.WriteFile(filepath.Join(e.receiptDir, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				if l, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if l != nil {
						l.Close()
					}
					t.Fatal("orphan admitted mutation")
				}
				if m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if m != nil {
						m.Close()
					}
					t.Fatal("orphan adopted owner")
				}
				if err := ReplayRuntimeResult(e.receiptDir, b.RuntimeResultBinding, func(string, []byte) error { t.Error("unexpected replay"); return nil }); err != ErrRuntimeMutationBlocked {
					t.Fatal(err)
				}
				_ = g
			})
		}
	}
}
