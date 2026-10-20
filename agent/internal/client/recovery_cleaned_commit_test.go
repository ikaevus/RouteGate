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
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestCleanedCommitSubmissionClient(t *testing.T) {
	for _, mode := range []string{"ok", "uncertain", "retry", "wrong_ack", "denied", "credential", "authorization", "runtime", "substitution", "cancelled", "missing_intent", "historical_missing_ack", "historical_identity", "historical_changed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dir string
			var v VerifiedRecoveryReleaseAuthorization
			var prepared tasks.RuntimeRecoveryPreparedReleaseProof
			var retention tasks.RuntimeRecoveryRetentionProof
			var adapter *retentionTestAdapter
			var historical atomic.Bool
			var posts, verifies atomic.Int32
			var frozen []byte
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("transport authentication")
				}
				identity := strings.HasSuffix(r.URL.Path, "/identity/verify")
				if !(historical.Load() && identity) {
					f, e := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_RDWR, 0)
					if e != nil {
						t.Error(e)
						w.WriteHeader(500)
						return
					}
					defer f.Close()
					e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
					if e == nil {
						syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
					}
					if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
						t.Error("HTTP escaped original flock", e)
					}
				}
				w.Header().Set("Cache-Control", "no-store")
				if identity {
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					gen := v.Binding.AgentCredentialGeneration
					if mode == "credential" || historical.Load() && mode == "historical_identity" {
						gen++
					}
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, gen, recoveryReadHash(data)})
					return
				}
				base := "/api/v1/agent/recovery-observations/" + prepared.OperationID
				switch r.URL.Path {
				case base + "/prepared-release/verify":
					h := recoveryReadHash(data)
					json.NewEncoder(w).Encode(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: prepared.OperationID, ReleaseIntentHash: prepared.ReleaseIntentHash, ProofHash: h, RequestHash: h})
				case base + "/release-authorization":
					a := v.Authorization
					if mode == "authorization" {
						a.AgentCredentialGeneration++
					}
					json.NewEncoder(w).Encode(recoveryReleaseReply{1, prepared.OperationID, v.Binding.AgentID, v.Binding.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data), v.AuthorizationHash, a})
				case base + "/retention/verify":
					wire, _ := json.Marshal(retention)
					if !bytes.Equal(data, wire) {
						t.Error("retention changed")
					}
					h := recoveryReadHash(data)
					json.NewEncoder(w).Encode(RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: retention.OperationID, RetentionHash: retention.RetentionHash, ProofHash: h, RequestHash: h})
				case base + "/cleaned-commit", base + "/cleaned-commit/verify":
					verify := strings.HasSuffix(r.URL.Path, "/verify")
					if verify {
						verifies.Add(1)
					} else {
						posts.Add(1)
					}
					saved, e := os.ReadFile(filepath.Join(dir, "recovery-cleaned-commit-submission.json"))
					if e != nil || !bytes.Equal(saved, data) {
						t.Error("HTTP before durable proof", e)
					}
					if frozen == nil {
						frozen = append([]byte(nil), data...)
					} else if !bytes.Equal(frozen, data) {
						t.Error("retry renewed bytes")
					}
					var p tasks.RuntimeRecoveryCleanedCommitProof
					json.Unmarshal(data, &p)
					if bytes.Contains(data, []byte(`"binding"`)) || bytes.Contains(data, []byte(`"files"`)) {
						t.Error("private inventory leaked")
					}
					if !historical.Load() {
						switch mode {
						case "runtime":
							adapter.mu.Lock()
							adapter.generation.PID++
							adapter.mu.Unlock()
						case "substitution":
							f := filepath.Join(dir, "recovery-cleaned-commit-submission.json")
							os.WriteFile(f+".new", data, 0600)
							os.Rename(f+".new", f)
						case "cancelled":
							cancel()
						}
					}
					if mode == "denied" || mode == "retry" && verifies.Load() == 1 && verify {
						w.WriteHeader(409)
						return
					}
					if !verify && (mode == "uncertain" || mode == "retry" && posts.Load() == 1) {
						w.WriteHeader(503)
						return
					}
					h := recoveryReadHash(data)
					reply := RecoveryCleanedCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, CleanedIntentHash: p.CleanedIntentHash, ProofHash: h, RequestHash: h}
					if mode == "wrong_ack" {
						reply.RequestHash = "invalid"
					}
					if historical.Load() && mode == "historical_changed" {
						reply.CleanedIntentHash = "invalid"
					}
					json.NewEncoder(w).Encode(reply)
				default:
					t.Error("unexpected route", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer s.Close()
			var owner *tasks.RuntimeRecoveryFinalization
			dir, owner, adapter, v, prepared = retentionClientOwner(t, s.URL)
			if e := owner.WithPreparedReleaseSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
				if _, e := g.RetainHistory(ctx); e != nil {
					return e
				}
				var e error
				retention, e = g.PinRetentionProof(ctx)
				if e != nil {
					return e
				}
				wire, _ := json.Marshal(retention)
				h := recoveryReadHash(wire)
				if e = g.RecordRetentionReceipt(ctx, RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: retention.OperationID, RetentionHash: retention.RetentionHash, ProofHash: h, RequestHash: h}); e != nil {
					return e
				}
				_, e = g.ArchiveRetainedHistory(ctx)
				return e
			}); e != nil {
				t.Fatal(e)
			}
			if e := owner.WithSourceCleanupSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
				if _, e := g.CleanupArchivedSources(ctx); e != nil {
					return e
				}
				if mode == "missing_intent" {
					return nil
				}
				_, e := g.PrepareCleanedCommit(ctx)
				return e
			}); e != nil {
				t.Fatal(e)
			}
			copyPath := dir + "-history-" + prepared.OperationID
			copyBefore := preparedHistoryTree(t, copyPath)
			c := New(s.URL)
			r, e := c.SubmitRecoveryCleanedCommit(ctx, "isolated-token", v, owner)
			success := mode == "ok" || mode == "uncertain" || mode == "retry" || strings.HasPrefix(mode, "historical_")
			if success {
				if e != nil || !r.Recorded {
					t.Fatal("submission", e)
				}
			} else if e != ErrRecoveryCleanedCommitUnconfirmed || r != (RecoveryCleanedCommitReceipt{}) {
				t.Fatal("accepted uncertainty", e)
			}
			if mode == "uncertain" && verifies.Load() == 0 || mode == "retry" && posts.Load() != 2 {
				t.Error("retry/readback missing")
			}
			if mode == "credential" || mode == "authorization" || mode == "missing_intent" {
				if posts.Load() != 0 {
					t.Error("failed prerequisite reached HTTP")
				}
			}
			owner.Close()
			for _, name := range []string{"mutation-inflight.json", "recovery-fence-release-intent.json"} {
				if _, e := os.Stat(filepath.Join(dir, name)); e != nil {
					t.Fatal("lost barrier", e)
				}
			}
			if success {
				historical.Store(true)
				if mode == "historical_missing_ack" {
					if e := os.Remove(filepath.Join(dir, "recovery-cleaned-commit-acknowledgement.json")); e != nil {
						t.Fatal(e)
					}
				}
				before := preparedHistoryTree(t, dir)
				oldPosts := posts.Load()
				got, e := c.ReconcileRecoveryCleanedCommitEvidence(ctx, "isolated-token", v.Binding.AgentID, v.Binding.ServerID, dir, prepared.OperationID)
				if mode == "historical_identity" || mode == "historical_changed" {
					if e != ErrRecoveryCleanedCommitUnconfirmed || got != (RecoveryCleanedCommitReceipt{}) {
						t.Fatal("historical mismatch", e)
					}
				} else if e != nil || got != r {
					t.Fatal("historical storage", e)
				}
				if posts.Load() != oldPosts {
					t.Error("historical path posted evidence")
				}
				if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
					t.Fatal("historical read changed files")
				}
			}
			if !reflect.DeepEqual(copyBefore, preparedHistoryTree(t, copyPath)) {
				t.Fatal("independent archive changed")
			}
		})
	}
}
