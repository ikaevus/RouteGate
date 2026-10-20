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

func TestCleanedCommitReadinessClient(t *testing.T) {
	testCleanedCommitReadinessClient(t, "plan")
}
func TestCleanedArchiveClient(t *testing.T)        { testCleanedCommitReadinessClient(t, "archive") }
func TestCleanedArchiveClientHistory(t *testing.T) { testCleanedCommitReadinessClient(t, "history") }
func TestAdmissionReleaseClientReadiness(t *testing.T) {
	testCleanedCommitReadinessClient(t, "release-readiness")
}
func TestAdmissionReleaseClientAuthority(t *testing.T) {
	testCleanedCommitReadinessClient(t, "release-authority")
}
func testCleanedCommitReadinessClient(t *testing.T, phase string) {
	modes := []string{"ok", "concurrent", "unacknowledged", "credential", "prepared", "retention", "cleaned_missing", "wrong_ack", "duplicate_ack", "redirect", "denied_before", "denied_after", "credential_after", "prepared_after", "authorization_after", "runtime", "proof_inode", "ack_inode", "archive_inode", "cancelled", "missing_local_ack", "binding"}
	if phase == "release-readiness" {
		modes = modes[:11]
	}
	if phase == "release-authority" {
		modes = append(modes[11:], "after_final_cancelled")
	}
	if phase == "history" {
		modes = []string{"historical_missing_sources", "historical_identity", "historical_ack", "historical_copy"}
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dir string
			var v VerifiedRecoveryReleaseAuthorization
			var prepared tasks.RuntimeRecoveryPreparedReleaseProof
			var retention tasks.RuntimeRecoveryRetentionProof
			var proof tasks.RuntimeRecoveryCleanedCommitProof
			var ack RecoveryCleanedCommitReceipt
			var adapter *retentionTestAdapter
			var identities, preparedReads, authorities, cleanedReads atomic.Int32
			var historical atomic.Bool
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("transport authentication")
				}
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
				if !(historical.Load() && strings.HasSuffix(r.URL.Path, "/identity/verify")) && e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
					t.Error("readiness HTTP escaped flock", e)
				}
				w.Header().Set("Cache-Control", "no-store")
				if strings.HasSuffix(r.URL.Path, "/identity/verify") {
					n := identities.Add(1)
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					gen := v.Binding.AgentCredentialGeneration
					if mode == "credential" || mode == "credential_after" && n > 1 || historical.Load() && mode == "historical_identity" {
						gen++
					}
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, gen, recoveryReadHash(data)})
					return
				}
				base := "/api/v1/agent/recovery-observations/" + prepared.OperationID
				switch r.URL.Path {
				case base + "/prepared-release/verify":
					n := preparedReads.Add(1)
					h := recoveryReadHash(data)
					if mode == "prepared" || mode == "prepared_after" && n > 1 {
						h = "wrong"
					}
					json.NewEncoder(w).Encode(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: prepared.OperationID, ReleaseIntentHash: prepared.ReleaseIntentHash, ProofHash: h, RequestHash: h})
				case base + "/release-authorization":
					n := authorities.Add(1)
					if mode == "denied_before" || mode == "denied_after" && n > 1 {
						w.WriteHeader(409)
						return
					}
					a := v.Authorization
					if mode == "authorization_after" && n > 1 {
						a.AgentCredentialGeneration++
					}
					json.NewEncoder(w).Encode(recoveryReleaseReply{1, prepared.OperationID, v.Binding.AgentID, v.Binding.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data), v.AuthorizationHash, a})
				case base + "/retention/verify":
					h := recoveryReadHash(data)
					if mode == "retention" {
						h = "wrong"
					}
					json.NewEncoder(w).Encode(RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: retention.OperationID, RetentionHash: retention.RetentionHash, ProofHash: h, RequestHash: h})
				case base + "/cleaned-commit/verify":
					cleanedReads.Add(1)
					wire, _ := json.Marshal(proof)
					if !bytes.Equal(data, wire) {
						t.Error("changed cleaned proof")
					}
					if mode == "cleaned_missing" {
						w.WriteHeader(409)
						return
					}
					if mode == "redirect" {
						http.Redirect(w, r, "/unexpected", 307)
						return
					}
					replace := func(path string) {
						data, e := os.ReadFile(path)
						if e != nil {
							t.Error(e)
							return
						}
						if e = os.WriteFile(path+".new", data, 0600); e != nil {
							t.Error(e)
						}
						if e = os.Rename(path+".new", path); e != nil {
							t.Error(e)
						}
					}
					switch mode {
					case "runtime":
						adapter.mu.Lock()
						adapter.generation.PID++
						adapter.mu.Unlock()
					case "proof_inode":
						replace(filepath.Join(dir, "recovery-cleaned-commit-submission.json"))
					case "ack_inode":
						replace(filepath.Join(dir, "recovery-cleaned-commit-acknowledgement.json"))
					case "archive_inode":
						replace(filepath.Join(dir+"-history-"+proof.OperationID, "manifest.json"))
					case "cancelled":
						cancel()
					}
					reply := ack
					if historical.Load() && mode == "historical_ack" {
						reply.ProofHash = "wrong"
					}
					if historical.Load() && mode == "historical_copy" {
						replace(filepath.Join(dir+"-cleaned-history-"+proof.OperationID, "manifest.json"))
					}
					if mode == "wrong_ack" {
						reply.RequestHash = "wrong"
					}
					if mode == "duplicate_ack" {
						wire, _ := json.Marshal(reply)
						w.Write(append(wire[:len(wire)-1], []byte(`,"recorded":true}`)...))
						return
					}
					json.NewEncoder(w).Encode(reply)
				default:
					t.Error("unexpected mutation/route", r.URL.Path)
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
				if _, e := g.PrepareCleanedCommit(ctx); e != nil {
					return e
				}
				var e error
				proof, e = g.PinCleanedCommitProof(ctx)
				if e != nil {
					return e
				}
				wire, _ := json.Marshal(proof)
				h := recoveryReadHash(wire)
				ack = RecoveryCleanedCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: proof.OperationID, CleanedIntentHash: proof.CleanedIntentHash, ProofHash: h, RequestHash: h}
				if mode == "unacknowledged" {
					return nil
				}
				return g.RecordCleanedCommitReceipt(ctx, ack)
			}); e != nil {
				t.Fatal(e)
			}
			if strings.HasPrefix(phase, "release-") && mode != "unacknowledged" {
				if e := owner.WithSourceCleanupSubmission(ctx, v.Binding, v.AuthorizationHash, func(g *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
					_, e := g.ArchiveCleanedCommitHistory(ctx)
					return e
				}); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "missing_local_ack" {
				os.Remove(filepath.Join(dir, "recovery-cleaned-commit-acknowledgement.json"))
			}
			before := preparedHistoryTree(t, dir)
			archive := dir + "-history-" + proof.OperationID
			backup := preparedHistoryTree(t, archive)
			c := New(s.URL)
			input := v
			if mode == "binding" {
				input.Binding.AgentCredentialGeneration++
			}
			if strings.HasPrefix(phase, "release-") {
				if mode == "after_final_cancelled" {
					adapter.mu.Lock()
					adapter.onObserve = func() {
						if _, e := os.Lstat(filepath.Join(dir, "mutation-inflight.json")); os.IsNotExist(e) {
							cancel()
						}
					}
					adapter.mu.Unlock()
				}
				copyPath := dir + "-cleaned-history-" + proof.OperationID
				var copyBefore map[string][]byte
				if mode != "unacknowledged" {
					copyBefore = preparedHistoryTree(t, copyPath)
				}
				type reply struct {
					result tasks.RuntimeRecoveryAdmissionReleaseResult
					err    error
				}
				replies := make(chan reply, 2)
				call := func() {
					r, e := c.ReleaseRecoveryAdmission(ctx, "isolated-token", input, owner)
					replies <- reply{r, e}
				}
				calls := 1
				if mode == "concurrent" {
					calls = 2
					go call()
					go call()
				} else {
					call()
				}
				successes := 0
				for range calls {
					r := <-replies
					if r.err == nil {
						successes++
						if r.result.Outcome != tasks.RuntimeRecoveryAdmissionDurable || r.result.RemovedFiles != 5 || r.result.OperationID != proof.OperationID || len(r.result.ArchiveHash) != 64 {
							t.Fatal("release result", r)
						}
					} else if mode == "after_final_cancelled" {
						if r.err != ErrRecoveryCleanedCommitUnconfirmed || r.result.Outcome != tasks.RuntimeRecoveryAdmissionUncertain || r.result.RemovedFiles != 5 {
							t.Fatal("lost final unlink uncertainty", r)
						}
					} else if r.err != ErrRecoveryCleanedCommitUnconfirmed || r.result.Outcome != tasks.RuntimeRecoveryAdmissionNotCommitted {
						t.Fatal("unsafe release result", r)
					}
				}
				if mode == "ok" || mode == "concurrent" {
					if successes != 1 {
						t.Fatal("release was not one-shot", successes)
					}
					if authorities.Load() != 2 || identities.Load() != 2 || cleanedReads.Load() != 1 {
						t.Fatal("current authority missing after historical storage")
					}
					entries, e := os.ReadDir(dir)
					if e != nil || len(entries) != 1 || entries[0].Name() != "runtime.lock" {
						t.Fatal("root records not released", e)
					}
					if lease, e := tasks.BeginRuntimeMutation(dir, proof.Observation.TaskID, tasks.TaskKindVPNCoreService); e != tasks.ErrRuntimeMutationBlocked {
						if lease != nil {
							lease.Close()
						}
						t.Fatal("flock dropped before Close")
					}
					owner.Close()
					lease, e := tasks.BeginRuntimeMutation(dir, proof.Observation.TaskID, tasks.TaskKindVPNCoreService)
					if e != nil {
						t.Fatal("durable release blocked", e)
					}
					lease.Complete()
					lease.Close()
				} else if mode == "after_final_cancelled" {
					if successes != 0 {
						t.Fatal("uncertain release reported success")
					}
					if _, e := os.Lstat(filepath.Join(dir, "mutation-inflight.json")); !os.IsNotExist(e) {
						t.Fatal("fixture missed last unlink")
					}
				} else if successes != 0 || !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
					t.Fatal("failed readiness removed sources")
				}
				if !reflect.DeepEqual(backup, preparedHistoryTree(t, archive)) {
					t.Fatal("changed predecessor archive")
				}
				if copyBefore != nil && !reflect.DeepEqual(copyBefore, preparedHistoryTree(t, copyPath)) {
					t.Fatal("changed cleaned archive")
				}
				return
			}
			results := make(chan tasks.RuntimeRecoveryCleanedCommitPlan, 2)
			errs := make(chan error, 2)
			hashes := make(chan string, 2)
			call := func() {
				var p tasks.RuntimeRecoveryCleanedCommitPlan
				var hash string
				var e error
				if phase == "plan" {
					p, e = c.PlanRecoveryCleanedCommit(ctx, "isolated-token", input, owner)
				} else {
					hash, e = c.ArchiveRecoveryCleanedCommitHistory(ctx, "isolated-token", input, owner)
				}
				results <- p
				errs <- e
				hashes <- hash
			}
			calls := 1
			if mode == "concurrent" {
				calls = 2
				go call()
				go call()
			} else {
				call()
			}
			var archiveHash string
			for range calls {
				p, e := <-results, <-errs
				hash := <-hashes
				if mode == "ok" || mode == "concurrent" || phase == "history" {
					if phase != "plan" {
						if e != nil || len(hash) != 64 || archiveHash != "" && hash != archiveHash {
							t.Fatal("archive result", e)
						}
						archiveHash = hash
						continue
					}
					if e != nil || p.ProofHash != ack.ProofHash || p.ReceiptHash == "" || p.Binding != v.Binding || p.AuthorizationHash != v.AuthorizationHash || p.ExpiresAt != v.Authorization.ExpiresAt || p.ObservedAt.Before(proof.ObservedAt) {
						t.Fatal("unbound readiness", e)
					}
					for _, f := range p.Files {
						if f.Hash != recoveryReadHash(before[f.Name]) || f.Name == "runtime.lock" {
							t.Fatal("unsafe planned file")
						}
					}
				} else if e != ErrRecoveryCleanedCommitUnconfirmed || p != (tasks.RuntimeRecoveryCleanedCommitPlan{}) || hash != "" {
					t.Fatal("accepted unsafe readiness", e)
				}
			}
			if mode == "ok" && (authorities.Load() != 2 || identities.Load() != 2 || cleanedReads.Load() != 1) {
				t.Fatal("authority not rechecked after historical storage")
			}
			if mode == "unacknowledged" && cleanedReads.Load() != 0 {
				t.Fatal("unowned acknowledgement reached Manager readback")
			}
			if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) || !reflect.DeepEqual(backup, preparedHistoryTree(t, archive)) {
				t.Fatal("readiness changed audit/files")
			}
			if phase == "archive" && mode != "ok" && mode != "concurrent" {
				if _, e := os.Lstat(dir + "-cleaned-history-" + proof.OperationID); !os.IsNotExist(e) {
					t.Fatal("archive written before confirmed readiness", e)
				}
			}
			if phase == "history" {
				owner.Close()
				historical.Store(true)
				if mode == "historical_missing_sources" {
					for _, name := range []string{"recovery-cleaned-commit-intent.json", "recovery-cleaned-commit-submission.json", "recovery-cleaned-commit-acknowledgement.json"} {
						if e := os.Remove(filepath.Join(dir, name)); e != nil {
							t.Fatal(e)
						}
					}
				}
				before = preparedHistoryTree(t, dir)
				path := dir + "-cleaned-history-" + proof.OperationID
				copyBefore := preparedHistoryTree(t, path)
				hash, e := c.ReconcileArchivedRecoveryCleanedCommit(ctx, "isolated-token", v.Binding.AgentID, v.Binding.ServerID, dir, proof.OperationID)
				if mode == "historical_missing_sources" {
					if e != nil || hash != archiveHash {
						t.Fatal("archived reconciliation", e)
					}
				} else if e != ErrRecoveryCleanedCommitUnconfirmed || hash != "" {
					t.Fatal("unsafe archived reconciliation", e)
				}
				if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) || !reflect.DeepEqual(copyBefore, preparedHistoryTree(t, path)) || !reflect.DeepEqual(backup, preparedHistoryTree(t, archive)) {
					t.Fatal("historical reconciliation changed files")
				}
			}
		})
	}
}
