package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCleanedCommitSubmissionOwner(t *testing.T) {
	e, _, b, m, path := cleanedCommitFixture(t)
	intent := prepareCleanedCommit(t, b, m)
	before, copyBefore := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, path)
	var first RuntimeRecoveryCleanedCommitProof
	var receipt RuntimeRecoveryCleanedCommitReceipt
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	for range 2 {
		if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			p, err := g.PinCleanedCommitProof(context.Background())
			if err != nil {
				return err
			}
			wire, _ := json.Marshal(p)
			if first.SchemaVersion == 0 {
				first = p
			} else if p != first {
				t.Error("proof renewed")
			}
			if p.CleanedIntentHash != markerDigest(m.cleanedCommitIntent) || p.IntentAt != intent.ObservedAt || p.Observation != intent.Observation {
				t.Error("unbound proof")
			}
			receipt = RuntimeRecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, markerDigest(wire), markerDigest(wire)}
			return g.RecordCleanedCommitReceipt(context.Background(), receipt)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := escaped.PinCleanedCommitProof(context.Background()); err != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryCleanedCommitProof{}) {
		t.Fatal("escaped owner", err)
	}
	before[runtimeRecoveryCleanedCommitSubmissionFile], _ = json.Marshal(first)
	before[runtimeRecoveryCleanedCommitAcknowledgementFile], _ = json.Marshal(receipt)
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, copyBefore)
	m.Close()
	os.Remove(e.activePath)
	if err := WithRecoveryCleanedCommitEvidenceHistory(context.Background(), m.dir, first.OperationID, b, func(p RuntimeRecoveryCleanedCommitProof, r RuntimeRecoveryCleanedCommitReceipt) error {
		if p != first || r != receipt {
			t.Error("history drift")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if lease, err := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("fence released")
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, copyBefore)
}

func TestCleanedCommitSubmissionConcurrent(t *testing.T) {
	_, _, b, m, _ := cleanedCommitFixture(t)
	prepareCleanedCommit(t, b, m)
	var wg sync.WaitGroup
	proofs := make(chan RuntimeRecoveryCleanedCommitProof, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				p, e := g.PinCleanedCommitProof(context.Background())
				if e != nil {
					return e
				}
				proofs <- p
				wire, _ := json.Marshal(p)
				h := markerDigest(wire)
				return g.RecordCleanedCommitReceipt(context.Background(), RuntimeRecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, h, h})
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(proofs)
	var first RuntimeRecoveryCleanedCommitProof
	for p := range proofs {
		if first.SchemaVersion == 0 {
			first = p
		} else if p != first {
			t.Fatal("concurrent renewal")
		}
	}
}

func TestCleanedCommitSubmissionInvalidReceipt(t *testing.T) {
	_, _, b, m, _ := cleanedCommitFixture(t)
	prepareCleanedCommit(t, b, m)
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.PinCleanedCommitProof(context.Background())
		if e != nil {
			return e
		}
		wire, _ := json.Marshal(p)
		h := markerDigest(wire)
		valid := RuntimeRecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, h, h}
		for i := range 6 {
			r := valid
			switch i {
			case 0:
				r.SchemaVersion++
			case 1:
				r.Recorded = false
			case 2:
				r.OperationID = "wrong"
			case 3:
				r.CleanedIntentHash = "wrong"
			case 4:
				r.ProofHash = "wrong"
			case 5:
				r.RequestHash = "wrong"
			}
			if e := g.RecordCleanedCommitReceipt(context.Background(), r); e != ErrRuntimeMutationBlocked {
				t.Error("invalid ack", i, e)
			}
			if !runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryCleanedCommitAcknowledgementFile)) {
				t.Error("invalid ack persisted")
			}
		}
		return g.RecordCleanedCommitReceipt(context.Background(), valid)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanedCommitEvidenceHistoryRefusals(t *testing.T) {
	for _, mode := range []string{"expired", "missing_ack", "missing_proof", "missing_intent", "wrong_proof", "wrong_ack", "cancelled", "generation", "callback_inode", "callback_archive", "unknown", "proof_before_intent"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m, path := cleanedCommitFixture(t)
			prepareCleanedCommit(t, b, m)
			var p RuntimeRecoveryCleanedCommitProof
			if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var e error
				p, e = g.PinCleanedCommitProof(context.Background())
				if e != nil {
					return e
				}
				wire, _ := json.Marshal(p)
				h := markerDigest(wire)
				return g.RecordCleanedCommitReceipt(context.Background(), RuntimeRecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, h, h})
			}); err != nil {
				t.Fatal(err)
			}
			if mode == "expired" {
				m.now = func() time.Time { return m.authorization.ExpiresAt }
				if e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error { t.Error("expired owner"); return nil }); e != ErrRuntimeMutationBlocked {
					t.Fatal(e)
				}
			}
			m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			proofPath := filepath.Join(m.dir, runtimeRecoveryCleanedCommitSubmissionFile)
			ackPath := filepath.Join(m.dir, runtimeRecoveryCleanedCommitAcknowledgementFile)
			switch mode {
			case "missing_ack":
				os.Remove(ackPath)
			case "missing_proof":
				os.Remove(proofPath)
			case "missing_intent":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile))
			case "wrong_proof":
				p.ManifestHash = "wrong"
				data, _ := json.Marshal(p)
				os.WriteFile(proofPath, data, 0600)
			case "wrong_ack":
				os.WriteFile(ackPath, []byte(`{}`), 0600)
			case "cancelled":
				cancel()
			case "generation":
				b.AgentCredentialGeneration++
			case "unknown":
				os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
			case "proof_before_intent":
				p.ObservedAt = p.IntentAt.Add(-time.Nanosecond)
				data, _ := json.Marshal(p)
				os.WriteFile(proofPath, data, 0600)
			}
			before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, path)
			err := WithRecoveryCleanedCommitEvidenceHistory(ctx, m.dir, p.OperationID, b, func(_ RuntimeRecoveryCleanedCommitProof, r RuntimeRecoveryCleanedCommitReceipt) error {
				if mode == "missing_ack" && r.Recorded {
					t.Error("invented acknowledgement")
				}
				if mode == "callback_inode" {
					replaceHistoryFile(t, proofPath)
				}
				if mode == "callback_archive" {
					replaceHistoryFile(t, filepath.Join(path, "manifest.json"))
				}
				return nil
			})
			if mode == "expired" || mode == "missing_ack" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe history", err)
			}
			assertTerminalHistoryTree(t, m.dir, before)
			assertTerminalHistoryTree(t, path, backup)
		})
	}
}

func TestCleanedCommitSubmissionAdmission(t *testing.T) {
	for _, name := range []string{runtimeRecoveryCleanedCommitSubmissionFile, runtimeRecoveryCleanedCommitAcknowledgementFile} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), []byte("malformed"), 0600); err != nil {
				t.Fatal(err)
			}
			if lease, err := BeginRuntimeMutation(dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
				if lease != nil {
					lease.Close()
				}
				t.Fatal("orphan evidence admitted mutation")
			}
		})
	}
}

func TestCleanedCommitSubmissionPublicationRefusals(t *testing.T) {
	for _, phase := range []string{"proof", "ack"} {
		for _, step := range []string{"before", "file", "linked", "unlinked-temp", "durable", "published", "done", "substitution", "runtime"} {
			t.Run(phase+"/"+step, func(t *testing.T) {
				_, a, b, m, path := cleanedCommitFixture(t)
				prepareCleanedCommit(t, b, m)
				backup := terminalHistoryTree(t, path)
				err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
					cut := func(at string) error {
						if at == step {
							return errors.New("private failure")
						}
						if at == "published" && step == "substitution" {
							name := runtimeRecoveryCleanedCommitSubmissionFile
							if phase == "ack" {
								name = runtimeRecoveryCleanedCommitAcknowledgementFile
							}
							replaceHistoryFile(t, filepath.Join(m.dir, name))
						}
						if at == "published" && step == "runtime" {
							a.generation.PID++
						}
						return nil
					}
					if phase == "proof" {
						p, e := g.pinCleanedCommitProof(context.Background(), cut)
						if p != (RuntimeRecoveryCleanedCommitProof{}) {
							t.Error("partial proof")
						}
						return e
					}
					p, e := g.PinCleanedCommitProof(context.Background())
					if e != nil {
						return e
					}
					wire, _ := json.Marshal(p)
					h := markerDigest(wire)
					return g.recordCleanedCommitReceipt(context.Background(), RuntimeRecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, h, h}, cut)
				})
				if err != ErrRuntimeMutationBlocked || !m.poisoned {
					t.Fatal("uncertain publication adopted", err)
				}
				for _, name := range []string{"mutation-inflight.json", runtimeRecoveryReleaseIntentFile} {
					if runtimeMarkerAbsent(filepath.Join(m.dir, name)) {
						t.Fatal("barrier deleted")
					}
				}
				assertTerminalHistoryTree(t, path, backup)
			})
		}
	}
}
