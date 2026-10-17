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
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestRetentionSubmissionClientOwner(t *testing.T) {
	for _, mode := range []string{"accepted", "concurrent", "lost_ack", "auth_denied", "bad_receipt", "drift_after_accept", "source_replaced", "cancelled_after_accept"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dir string
			var v VerifiedRecoveryReleaseAuthorization
			var prepared tasks.RuntimeRecoveryPreparedReleaseProof
			var a *retentionTestAdapter
			var submitting, historical atomic.Bool
			var mu sync.Mutex
			var saved []byte
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("missing authenticated evidence transport")
				}
				// Check the actual lock, even in phases where older readers reject
				// the inventory independently of lock ownership.
				if !(historical.Load() && r.URL.Path == "/api/v1/agent/identity/verify") {
					f, e := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_RDWR, 0)
					if e != nil {
						t.Error(e)
						w.WriteHeader(500)
						return
					}
					e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
					if e == nil {
						syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
						t.Error("HTTP lost original flock")
					}
					f.Close()
					if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
						t.Error("unexpected flock failure", e)
					}
				}
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data)})
					return
				}
				base := "/api/v1/agent/recovery-observations/" + prepared.OperationID
				if r.URL.Path == base+"/prepared-release/verify" {
					wire, _ := json.Marshal(prepared)
					if !bytes.Equal(wire, data) {
						t.Error("prepared proof changed")
					}
					h := recoveryReadHash(wire)
					json.NewEncoder(w).Encode(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: prepared.OperationID, ReleaseIntentHash: prepared.ReleaseIntentHash, ProofHash: h, RequestHash: h})
					return
				}
				if r.URL.Path == base+"/release-authorization" {
					if mode == "auth_denied" && submitting.Load() {
						http.Error(w, "private authority denial", 409)
						return
					}
					json.NewEncoder(w).Encode(recoveryReleaseReply{1, prepared.OperationID, v.Binding.AgentID, v.Binding.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data), v.AuthorizationHash, v.Authorization})
					return
				}
				verify := r.URL.Path == base+"/retention/verify"
				if !verify && r.URL.Path != base+"/retention" {
					t.Error("unexpected endpoint", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				pin, e := os.ReadFile(filepath.Join(dir, "recovery-retention-submission.json"))
				if e != nil || !bytes.Equal(pin, data) || len(data) > 8192 || bytes.Contains(data, []byte(`"files":`)) {
					t.Error("HTTP did not use exact durable public proof", e)
				}
				var p tasks.RuntimeRecoveryRetentionProof
				json.Unmarshal(data, &p)
				retained, _ := os.ReadFile(filepath.Join(dir, "recovery-fence-release-retention.json"))
				preparedWire, _ := json.Marshal(prepared)
				if p.RetentionHash != recoveryReadHash(retained) || p.PreparedProofHash != recoveryReadHash(preparedWire) {
					t.Error("history not bound")
				}
				mu.Lock()
				if saved != nil && !bytes.Equal(saved, data) {
					t.Error("retry changed frozen proof")
				}
				if !verify {
					saved = append([]byte(nil), data...)
				}
				hasSaved := saved != nil
				mu.Unlock()
				if verify && (!hasSaved || mode == "bad_receipt") {
					w.WriteHeader(409)
					return
				}
				if !verify {
					switch mode {
					case "lost_ack":
						http.Error(w, "private uncertain response", 500)
						return
					case "drift_after_accept":
						a.mu.Lock()
						a.generation.PID++
						a.mu.Unlock()
					case "cancelled_after_accept":
						cancel()
					case "source_replaced":
						path := filepath.Join(dir, "retired-"+p.OperationID, "recovery-final-submission.json")
						contents, _ := os.ReadFile(path)
						tmp := filepath.Join(filepath.Dir(path), ".replacement")
						os.WriteFile(tmp, contents, 0600)
						os.Rename(tmp, path)
					}
				}
				h := recoveryReadHash(data)
				out := RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, RetentionHash: p.RetentionHash, ProofHash: h, RequestHash: h}
				if mode == "bad_receipt" {
					out.ProofHash = "invalid"
				}
				json.NewEncoder(w).Encode(out)
			}))
			defer s.Close()
			var owner *tasks.RuntimeRecoveryFinalization
			dir, owner, a, v, prepared = retentionClientOwner(t, s.URL)
			c := New(s.URL)
			if _, err := c.RetainRecoveryFenceReleaseHistory(ctx, "isolated-token", v, owner); err != nil {
				t.Fatal(err)
			}
			before := preparedHistoryTree(t, dir)
			submitting.Store(true)
			var r RecoveryRetentionReceipt
			var err error
			if mode == "concurrent" {
				type response struct {
					receipt RecoveryRetentionReceipt
					err     error
				}
				ch := make(chan response, 2)
				for range 2 {
					go func() { r, e := c.SubmitRecoveryRetention(ctx, "isolated-token", v, owner); ch <- response{r, e} }()
				}
				first, second := <-ch, <-ch
				if first.err != nil || second.err != nil || first.receipt != second.receipt {
					t.Fatal("concurrent submission changed evidence", first.err, second.err)
				}
				r = first.receipt
			} else {
				r, err = c.SubmitRecoveryRetention(ctx, "isolated-token", v, owner)
			}
			want := mode == "accepted" || mode == "concurrent" || mode == "lost_ack"
			if want {
				if err != nil || !r.Recorded {
					t.Fatal("unconfirmed retained history", err)
				}
				again, e := c.SubmitRecoveryRetention(ctx, "isolated-token", v, owner)
				if e != nil || again != r {
					t.Fatal("retry renewed history", e)
				}
			} else if err != ErrRecoveryRetentionUnconfirmed || r != (RecoveryRetentionReceipt{}) {
				t.Fatal("unsafe positive result", err)
			}
			for _, name := range []string{"recovery-retention-submission.json", "recovery-retention-acknowledgement.json"} {
				data, e := os.ReadFile(filepath.Join(dir, name))
				if e == nil {
					before[name] = data
				}
				if name == "recovery-retention-acknowledgement.json" && (e == nil) != want {
					t.Fatal("wrong ack phase")
				}
				if name == "recovery-retention-submission.json" && (e == nil) != (mode != "auth_denied") {
					t.Fatal("wrong pin phase")
				}
			}
			if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
				t.Fatal("submission changed retained sources")
			}
			owner.Close()
			if want || mode == "drift_after_accept" {
				historical.Store(true)
				ack, e := c.ReconcileRecoveryRetention(context.Background(), "isolated-token", v.Binding.AgentID, v.Binding.ServerID, dir)
				if e != nil || !ack.Recorded {
					t.Fatal("historical storage unavailable", e)
				}
				if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
					t.Fatal("historical readback repaired receipt")
				}
			}
			if l, e := tasks.BeginRuntimeMutation(dir, prepared.Observation.TaskID, tasks.TaskKindVPNCoreService); e != tasks.ErrRuntimeMutationBlocked {
				if l != nil {
					l.Close()
				}
				t.Fatal("storage acknowledgement released fence")
			}
		})
	}
}
