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
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/platform"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

type retentionTestAdapter struct {
	tasks.VPNCoreAdapter
	mu         sync.Mutex
	generation tasks.RuntimeGeneration
	port       int
}

func (a *retentionTestAdapter) Descriptor() platform.VPNCoreAdapterDescriptor {
	return platform.VPNCoreAdapterDescriptor{Core: "sing-box", Protocol: "vless"}
}
func (a *retentionTestAdapter) ObserveGeneration(context.Context, string) (tasks.RuntimeGeneration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generation, nil
}
func (a *retentionTestAdapter) IsEnabled(context.Context) (tasks.ServiceResult, error) {
	return tasks.ServiceResult{}, nil
}
func (a *retentionTestAdapter) CheckHealth(context.Context, string) (tasks.ListenerHealthResult, error) {
	return tasks.ListenerHealthResult{Port: a.port}, nil
}

// Reuse exact private wire fixtures only for the pre-finalization history;
// construct all subsequent files through the real live owner APIs.
func retentionClientOwner(t *testing.T, manager string) (string, *tasks.RuntimeRecoveryFinalization, *retentionTestAdapter, VerifiedRecoveryReleaseAuthorization, tasks.RuntimeRecoveryPreparedReleaseProof) {
	t.Helper()
	ctx := context.Background()
	g := recoveryReadTestGrant(t)
	g.IssuedAt = time.Now().UTC().Add(-10 * time.Second)
	g.ExpiresAt = g.IssuedAt.Add(time.Minute)
	activeData := []byte(`{"inbounds":[]}`)
	g.Expected.Witness.RuntimeHash = recoveryReadHash(activeData)
	b := tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: manager, AgentID: g.Expected.AgentID, ServerID: g.Expected.ServerID}, AgentCredentialGeneration: 7}
	dir, _, _ := writeRecoveryFinalHistoryFixture(t, b, g, "with_ack")
	data, err := os.ReadFile(filepath.Join(dir, "recovery-final-consumption.json"))
	if err != nil {
		t.Fatal(err)
	}
	var consumed struct {
		Authorization tasks.RuntimeRecoveryReleaseAuthorization
	}
	if err := json.Unmarshal(data, &consumed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"recovery-final-consumption.json", "recovery-final-submission.json", "recovery-final-acknowledgement.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(t.TempDir(), "active.json")
	if err := os.WriteFile(active, activeData, 0600); err != nil {
		t.Fatal(err)
	}
	a := &retentionTestAdapter{generation: g.Expected.Witness.Generation, port: g.Expected.Witness.ListenerPort}
	wire, _ := json.Marshal(consumed.Authorization)
	v := VerifiedRecoveryReleaseAuthorization{Binding: b, Authorization: consumed.Authorization, AuthorizationHash: recoveryReadHash(wire)}
	m, err := tasks.BeginRuntimeRecoveryFinalization(ctx, dir, b, wire, active, a)
	if err != nil {
		t.Fatal("live final owner", err)
	}
	t.Cleanup(func() { m.Close() })
	if err := m.WithSubmission(ctx, b, v.AuthorizationHash, func(guard *tasks.RuntimeRecoveryFinalizationGuard) error {
		p, err := guard.PinProof(ctx)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(p)
		hash := recoveryReadHash(data)
		if err := guard.RecordReceipt(ctx, tasks.RuntimeRecoveryFinalizationReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, AuthorizationHash: p.AuthorizationHash, FinalConsumptionHash: p.FinalConsumptionHash, ProofHash: hash, RequestHash: hash}); err != nil {
			return err
		}
		_, err = guard.PrepareRetirement(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RetirePrepared(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.WithTerminalSubmission(ctx, b, v.AuthorizationHash, func(guard *tasks.RuntimeRecoveryTerminalGuard) error {
		p, err := guard.PinProof(ctx)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(p)
		hash := recoveryReadHash(data)
		return guard.RecordReceipt(ctx, tasks.RuntimeRecoveryTerminalReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, TerminalHash: p.TerminalHash, ProofHash: hash, RequestHash: hash})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareFenceRelease(ctx, b, v.AuthorizationHash); err != nil {
		t.Fatal(err)
	}
	var prepared tasks.RuntimeRecoveryPreparedReleaseProof
	if err := m.WithPreparedReleaseSubmission(ctx, b, v.AuthorizationHash, func(guard *tasks.RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		prepared, err = guard.PinProof(ctx)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(prepared)
		hash := recoveryReadHash(data)
		return guard.RecordReceipt(ctx, tasks.RuntimeRecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: prepared.OperationID, ReleaseIntentHash: prepared.ReleaseIntentHash, ProofHash: hash, RequestHash: hash})
	}); err != nil {
		t.Fatal(err)
	}
	return dir, m, a, v, prepared
}

func TestReleaseRetentionClientHeldOwner(t *testing.T) {
	for _, mode := range []string{"retained", "denied", "drift", "cancelled", "source_replaced"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var v VerifiedRecoveryReleaseAuthorization
			var p tasks.RuntimeRecoveryPreparedReleaseProof
			var dir string
			var a *retentionTestAdapter
			var calls atomic.Int32
			var historical atomic.Bool
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				data, _ := io.ReadAll(r.Body)
				if r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("missing authenticated read")
				}
				if !historical.Load() || r.URL.Path != "/api/v1/agent/identity/verify" {
					if err := tasks.WithHistoricalRecoveryPreparedSubmission(context.Background(), dir, v.Binding, func(tasks.RuntimeRecoveryPreparedSubmissionHistory) error {
						t.Error("HTTP dropped live owner flock")
						return nil
					}); err != tasks.ErrRuntimeMutationBlocked {
						t.Error("HTTP not under original flock")
					}
				}
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data)})
					return
				}
				base := "/api/v1/agent/recovery-observations/" + p.OperationID
				if r.URL.Path == base+"/prepared-release/verify" {
					pin, _ := json.Marshal(p)
					if !bytes.Equal(data, pin) {
						t.Error("changed proof")
					}
					hash := recoveryReadHash(data)
					json.NewEncoder(w).Encode(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, ReleaseIntentHash: p.ReleaseIntentHash, ProofHash: hash, RequestHash: hash})
					return
				}
				if r.URL.Path != base+"/release-authorization" {
					t.Error("unexpected mutation endpoint")
					w.WriteHeader(404)
					return
				}
				if mode == "denied" {
					http.Error(w, "sensitive denied body", 409)
					return
				}
				if mode == "drift" {
					a.mu.Lock()
					a.generation.PID++
					a.mu.Unlock()
				}
				if mode == "source_replaced" {
					path := filepath.Join(dir, "retired-"+p.OperationID, "recovery-final-submission.json")
					data, err := os.ReadFile(path)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					tmp := filepath.Join(filepath.Dir(path), ".replacement")
					if err := os.WriteFile(tmp, data, 0600); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					if err := os.Rename(tmp, path); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
				}
				if mode == "cancelled" {
					cancel()
				}
				json.NewEncoder(w).Encode(recoveryReleaseReply{1, p.OperationID, v.Binding.AgentID, v.Binding.ServerID, v.Binding.AgentCredentialGeneration, recoveryReadHash(data), v.AuthorizationHash, v.Authorization})
			}))
			defer s.Close()
			var m *tasks.RuntimeRecoveryFinalization
			dir, m, a, v, p = retentionClientOwner(t, s.URL)
			before := preparedHistoryTree(t, dir)
			hash, err := New(s.URL).RetainRecoveryFenceReleaseHistory(ctx, "isolated-token", v, m)
			if mode == "retained" {
				if err != nil || len(hash) != 64 {
					t.Fatal("retention unconfirmed", err)
				}
				data, err := os.ReadFile(filepath.Join(dir, "recovery-fence-release-retention.json"))
				if err != nil || recoveryReadHash(data) != hash {
					t.Fatal("retention not durable", err)
				}
				before["recovery-fence-release-retention.json"] = data
				again, err := New(s.URL).RetainRecoveryFenceReleaseHistory(ctx, "isolated-token", v, m)
				if err != nil || again != hash {
					t.Fatal("retention retry changed history", err)
				}
			} else if err != ErrRecoveryReleaseNotReady || hash != "" {
				t.Fatal("unsafe retention", err)
			}
			if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
				t.Fatal("retention mutated original sources")
			}
			if (mode == "retained" && calls.Load() != 6) || (mode != "retained" && calls.Load() != 3) {
				t.Fatal("unexpected requests", calls.Load())
			}
			m.Close()
			if mode == "retained" {
				historical.Store(true)
				if receipt, err := New(s.URL).ReconcileRecoveryPreparedSubmission(context.Background(), "isolated-token", v.Binding.AgentID, v.Binding.ServerID, dir); err != nil || !receipt.Recorded {
					t.Fatal("retained historical readback", err)
				}
			}
			if lease, err := tasks.BeginRuntimeMutation(dir, p.Observation.TaskID, tasks.TaskKindVPNCoreService); err != tasks.ErrRuntimeMutationBlocked {
				if lease != nil {
					lease.Close()
				}
				t.Fatal("retention released mutation", err)
			}
		})
	}
}
