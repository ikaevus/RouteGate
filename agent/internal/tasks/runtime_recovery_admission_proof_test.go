package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pinAdmissionForTest(t *testing.T, b RuntimeRecoveryGrantBinding, m *RuntimeRecoveryFinalization, cut func(string) error) (RuntimeRecoveryAdmissionReleaseProof, error) {
	t.Helper()
	var p RuntimeRecoveryAdmissionReleaseProof
	e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		if _, e := g.ReleaseAdmission(context.Background()); e != nil {
			return e
		}
		var e error
		p, e = g.pinAdmissionReleaseProof(context.Background(), cut)
		return e
	})
	return p, e
}

func TestAdmissionProofComplete(t *testing.T) {
	_, _, b, m, previous, path := cleanedArchiveFixture(t)
	hash := archiveCleanedForTest(t, b, m)
	old, copy := terminalHistoryTree(t, previous), terminalHistoryTree(t, path)
	var p RuntimeRecoveryAdmissionReleaseProof
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	if e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		escaped = g
		if _, e := g.ReleaseAdmission(context.Background()); e != nil {
			return e
		}
		var e error
		p, e = g.PinAdmissionReleaseProof(context.Background())
		if e != nil {
			return e
		}
		if p.CleanedArchiveHash != hash || p.OperationID != m.authorization.OperationID || p.AuthorizationHash != m.authorizationHash || p.AgentCredentialGeneration != b.AgentCredentialGeneration || p.Observation != m.authorization.Expected || p.ReleasedAt != m.admissionRelease.completedAt || p.ObservedAt.Before(p.ReleasedAt) {
			t.Error("unbound release proof")
		}
		for range 2 {
			q, e := g.PinAdmissionReleaseProof(context.Background())
			if e != nil || q != p {
				t.Error("proof retry renewed bytes/time", e)
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	guardCopy := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &guardCopy} {
		if q, e := g.PinAdmissionReleaseProof(context.Background()); e != ErrRuntimeMutationBlocked || q != (RuntimeRecoveryAdmissionReleaseProof{}) {
			t.Fatal("escaped proof", e)
		}
	}
	m.Close()
	proofPath, _ := admissionReleaseProofPath(m.dir, p.OperationID)
	wire, _ := json.Marshal(p)
	if data, e := os.ReadFile(proofPath); e != nil || string(data) != string(wire) {
		t.Fatal("proof not retained", e)
	}
	if e := WithRecoveryAdmissionReleaseHistory(context.Background(), m.dir, p.OperationID, b, func(q RuntimeRecoveryAdmissionReleaseProof) error {
		if q != p {
			t.Error("history changed")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService)
	if e != nil {
		t.Fatal("proof blocked admission", e)
	}
	lease.Complete()
	lease.Close()
	assertTerminalHistoryTree(t, previous, old)
	assertTerminalHistoryTree(t, path, copy)
}

func TestAdmissionProofPublication(t *testing.T) {
	for _, mode := range []string{"before", "file", "linked", "unlinked-temp", "durable", "published", "done", "existing", "published_inode"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m, previous, path := cleanedArchiveFixture(t)
			archiveCleanedForTest(t, b, m)
			old, copy := terminalHistoryTree(t, previous), terminalHistoryTree(t, path)
			proofPath, _ := admissionReleaseProofPath(m.dir, m.authorization.OperationID)
			if mode == "existing" {
				if e := os.WriteFile(proofPath, []byte("private existing record"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			p, e := pinAdmissionForTest(t, b, m, func(step string) error {
				if mode == "published_inode" && step == "published" {
					replaceHistoryFile(t, proofPath)
				}
				if step == mode {
					return errors.New("private failure")
				}
				return nil
			})
			if e != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryAdmissionReleaseProof{}) || !m.poisoned {
				t.Fatal("unsafe publication", e)
			}
			if !runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) {
				t.Fatal("proof failure recreated barrier")
			}
			if mode == "existing" {
				data, _ := os.ReadFile(proofPath)
				if string(data) != "private existing record" {
					t.Fatal("adopted existing proof")
				}
			}
			m.Close()
			lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService)
			if e != nil {
				t.Fatal("proof failure changed admission", e)
			}
			lease.Complete()
			lease.Close()
			assertTerminalHistoryTree(t, previous, old)
			assertTerminalHistoryTree(t, path, copy)
		})
	}
}

func TestAdmissionProofDrift(t *testing.T) {
	for _, mode := range []string{"inode", "runtime", "expiry", "cancelled", "backward", "root"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m, _, _ := cleanedArchiveFixture(t)
			archiveCleanedForTest(t, b, m)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := m.WithSourceCleanupSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				if _, e := g.ReleaseAdmission(ctx); e != nil {
					return e
				}
				if _, e := g.PinAdmissionReleaseProof(ctx); e != nil {
					return e
				}
				path, _ := admissionReleaseProofPath(m.dir, m.authorization.OperationID)
				switch mode {
				case "inode":
					replaceHistoryFile(t, path)
				case "runtime":
					a.generation.PID++
				case "expiry":
					m.now = func() time.Time { return m.authorization.ExpiresAt }
				case "cancelled":
					cancel()
				case "backward":
					m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
				case "root":
					os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
				}
				p, e := g.PinAdmissionReleaseProof(ctx)
				if e != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryAdmissionReleaseProof{}) {
					t.Error("adopted drift", e)
				}
				return e
			})
			if e != ErrRuntimeMutationBlocked || !m.poisoned {
				t.Fatal("unsafe live proof", e)
			}
		})
	}
}

func TestAdmissionProofHistory(t *testing.T) {
	for _, mode := range []string{"expired_runtime_gone", "generation", "proof_inode_callback", "predecessor", "copy", "changed", "hardlink", "missing"} {
		t.Run(mode, func(t *testing.T) {
			ex, _, b, m, previous, copy := cleanedArchiveFixture(t)
			archiveCleanedForTest(t, b, m)
			p, e := pinAdmissionForTest(t, b, m, nil)
			if e != nil {
				t.Fatal(e)
			}
			m.Close()
			path, _ := admissionReleaseProofPath(m.dir, p.OperationID)
			switch mode {
			case "expired_runtime_gone":
				m.now = func() time.Time { return m.authorization.ExpiresAt }
				os.Remove(ex.activePath)
			case "generation":
				b.AgentCredentialGeneration++
			case "predecessor":
				os.Remove(filepath.Join(previous, "manifest.json"))
			case "copy":
				os.Remove(filepath.Join(copy, "manifest.json"))
			case "changed":
				os.WriteFile(path, []byte("{}"), 0600)
			case "hardlink":
				os.Link(path, filepath.Join(t.TempDir(), "linked"))
			case "missing":
				os.Remove(path)
			}
			e = WithRecoveryAdmissionReleaseHistory(context.Background(), m.dir, p.OperationID, b, func(q RuntimeRecoveryAdmissionReleaseProof) error {
				if q != p {
					t.Error("changed history")
				}
				if mode == "proof_inode_callback" {
					replaceHistoryFile(t, path)
				}
				return nil
			})
			if mode == "expired_runtime_gone" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe history", e)
			}
		})
	}
}

func TestRuntimeMutationAdmissionReleaseGolden(t *testing.T) {
	data, e := os.ReadFile("../../../testdata/recovery-admission-release-evidence-v1.json")
	var fixture struct{ Wire, SHA256 string }
	if e != nil || json.Unmarshal(data, &fixture) != nil {
		t.Fatal(e)
	}
	var p RuntimeRecoveryAdmissionReleaseProof
	if json.Unmarshal([]byte(fixture.Wire), &p) != nil || p.Purpose != RuntimeRecoveryAdmissionReleasePurpose {
		t.Fatal("fixture proof")
	}
	wire, e := json.Marshal(p)
	if e != nil || string(wire) != fixture.Wire || markerDigest(wire) != fixture.SHA256 {
		t.Fatal("Agent wire/hash drift")
	}
}
