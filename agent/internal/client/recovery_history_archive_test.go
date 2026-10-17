package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestRecoveryHistoryArchiveClientOwner(t *testing.T) {
	for _, mode := range []string{"accepted", "concurrent", "denied", "receipt", "runtime", "source", "cancelled", "historical_identity", "historical_receipt", "historical_copy"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dir string
			var v VerifiedRecoveryReleaseAuthorization
			var prepared tasks.RuntimeRecoveryPreparedReleaseProof
			var proof tasks.RuntimeRecoveryRetentionProof
			var a *retentionTestAdapter
			var historical atomic.Bool
			checkLock := func() {
				f, e := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_RDWR, 0)
				if e != nil {
					t.Error(e)
					return
				}
				defer f.Close()
				e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
				if e == nil {
					syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
					t.Error("HTTP escaped original admission flock")
				}
				if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
					t.Error("unexpected lock error", e)
				}
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("unauthenticated readback")
				}
				identity := r.URL.Path == "/api/v1/agent/identity/verify"
				if !(historical.Load() && identity) {
					checkLock()
				}
				if identity {
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					generation := v.Binding.AgentCredentialGeneration
					if historical.Load() && mode == "historical_identity" {
						generation++
					}
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, generation, recoveryReadHash(data)})
					return
				}
				base := "/api/v1/agent/recovery-observations/" + prepared.OperationID
				switch r.URL.Path {
				case base + "/prepared-release/verify":
					wire, _ := json.Marshal(prepared)
					if !bytes.Equal(data, wire) {
						t.Error("changed prepared proof")
					}
					h := recoveryReadHash(wire)
					json.NewEncoder(w).Encode(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: prepared.OperationID, ReleaseIntentHash: prepared.ReleaseIntentHash, ProofHash: h, RequestHash: h})
				case base + "/release-authorization":
					if mode == "denied" {
						w.WriteHeader(409)
						return
					}
					json.NewEncoder(w).Encode(recoveryReleaseReply{1, prepared.OperationID, v.Binding.AgentID, v.Binding.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data), v.AuthorizationHash, v.Authorization})
				case base + "/retention/verify":
					wire, _ := json.Marshal(proof)
					if !bytes.Equal(data, wire) || bytes.Contains(data, []byte(`"files":`)) {
						t.Error("private or changed archive proof")
					}
					if !historical.Load() {
						switch mode {
						case "runtime":
							a.mu.Lock()
							a.generation.PID++
							a.mu.Unlock()
						case "source":
							p := filepath.Join(dir, "recovery-retention-submission.json")
							d, _ := os.ReadFile(p)
							os.WriteFile(p+".new", d, 0600)
							os.Rename(p+".new", p)
						case "cancelled":
							cancel()
						}
					} else if mode == "historical_copy" {
						p := dir + "-history-" + proof.OperationID
						f := filepath.Join(p, "manifest.json")
						d, _ := os.ReadFile(f)
						os.WriteFile(f+".new", d, 0600)
						os.Rename(f+".new", f)
					}
					h := recoveryReadHash(data)
					r := RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: proof.OperationID, RetentionHash: proof.RetentionHash, ProofHash: h, RequestHash: h}
					if mode == "receipt" || historical.Load() && mode == "historical_receipt" {
						r.Recorded = false
					}
					json.NewEncoder(w).Encode(r)
				default:
					t.Error("unexpected write/route", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer s.Close()
			var owner *tasks.RuntimeRecoveryFinalization
			dir, owner, a, v, prepared = retentionClientOwner(t, s.URL)
			if err := owner.WithPreparedReleaseSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
				if _, err := g.RetainHistory(ctx); err != nil {
					return err
				}
				var err error
				proof, err = g.PinRetentionProof(ctx)
				if err != nil {
					return err
				}
				wire, _ := json.Marshal(proof)
				h := recoveryReadHash(wire)
				return g.RecordRetentionReceipt(ctx, tasks.RuntimeRecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: proof.OperationID, RetentionHash: proof.RetentionHash, ProofHash: h, RequestHash: h})
			}); err != nil {
				t.Fatal(err)
			}
			before := preparedHistoryTree(t, dir)
			c := New(s.URL)
			var hash string
			var err error
			if mode == "concurrent" {
				type result struct {
					hash string
					err  error
				}
				ch := make(chan result, 2)
				for range 2 {
					go func() { h, e := c.ArchiveRecoveryRetentionHistory(ctx, "isolated-token", v, owner); ch <- result{h, e} }()
				}
				first, second := <-ch, <-ch
				if first.err != nil || second.err != nil || first.hash != second.hash {
					t.Fatal("concurrent archive failed", first.err, second.err)
				}
				hash = first.hash
			} else {
				hash, err = c.ArchiveRecoveryRetentionHistory(ctx, "isolated-token", v, owner)
			}
			good := mode == "accepted" || mode == "concurrent" || len(mode) > 10 && mode[:11] == "historical_"
			if !good {
				if err != ErrRecoveryHistoryArchiveUnconfirmed || hash != "" {
					t.Fatal("unsafe live archive result", err)
				}
				if _, e := os.Lstat(dir + "-history-" + proof.OperationID); !os.IsNotExist(e) {
					t.Fatal("denial still copied history")
				}
			} else {
				if err != nil || len(hash) != 64 {
					t.Fatal("archive unavailable", err)
				}
				owner.Close()
				historical.Store(true)
				// Simulate future cleanup in tests only; keep original lock/fence.
				entries, _ := os.ReadDir(dir)
				for _, entry := range entries {
					if entry.Name() != "runtime.lock" && entry.Name() != "mutation-inflight.json" {
						if e := os.RemoveAll(filepath.Join(dir, entry.Name())); e != nil {
							t.Fatal(e)
						}
					}
				}
				remaining := preparedHistoryTree(t, dir)
				result, e := c.ReconcileArchivedRecoveryRetention(ctx, "isolated-token", v.Binding.AgentID, v.Binding.ServerID, dir, proof.OperationID)
				if mode == "accepted" || mode == "concurrent" {
					if e != nil || result.ManifestHash != hash || !result.Receipt.Recorded {
						t.Fatal("independent HTTP history unavailable", e)
					}
				} else if e != ErrRecoveryHistoryArchiveUnconfirmed || result != (RecoveryArchivedRetention{}) {
					t.Fatal("invalid historical result", e)
				}
				if !reflect.DeepEqual(remaining, preparedHistoryTree(t, dir)) {
					t.Fatal("historical read wrote original state")
				}
				return
			}
			if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
				t.Fatal("archive altered source bytes")
			}
		})
	}
}
