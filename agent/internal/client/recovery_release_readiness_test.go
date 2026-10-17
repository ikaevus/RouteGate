package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestReleaseReadinessTransport(t *testing.T) {
	for _, mode := range []string{"ready", "identity_error", "identity_generation", "identity_hash", "stored_missing", "stored_wrong", "stored_duplicate", "stored_redirect", "authority_revoked", "authority_missing", "authority_redirect", "authority_hash", "authority_request", "authority_operation", "authority_agent", "authority_server", "authority_generation", "authority_nonce", "authority_grant", "authority_consumption", "authority_commit", "authority_expected", "authority_expiry", "authority_issued", "authority_duplicate", "authority_alias", "authority_unknown", "authority_trailing", "authority_oversize", "drift_identity", "drift_stored", "drift_authority", "cancel_identity", "cancel_stored", "cancel_authority", "refresh_error", "expired", "future", "wrong_manager", "wrong_vhash"} {
		t.Run(mode, func(t *testing.T) {
			a := recoveryFinalizationTestAuthorization(t)
			if mode == "expired" {
				a.IssuedAt = time.Now().UTC().Add(-time.Minute)
				a.ExpiresAt = time.Now().UTC().Add(-time.Millisecond)
			}
			if mode == "future" {
				a.IssuedAt = time.Now().UTC().Add(time.Minute)
				a.ExpiresAt = a.IssuedAt.Add(time.Minute)
			}
			authority, _ := json.Marshal(a)
			p := tasks.RuntimeRecoveryPreparedReleaseProof{SchemaVersion: 1, Purpose: tasks.RuntimeRecoveryPreparedReleasePurpose, OperationID: a.OperationID, AgentCredentialGeneration: a.AgentCredentialGeneration, AuthorizationHash: recoveryReadHash(authority), TerminalProofHash: strings.Repeat("a", 64), TerminalReceiptHash: strings.Repeat("b", 64), ReleaseIntentHash: strings.Repeat("c", 64), PreparedAt: time.Now().UTC().Add(-time.Millisecond), ObservedAt: time.Now().UTC(), Observation: a.Expected}
			pin, _ := json.Marshal(p)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls, redirects atomic.Int32
			var drift atomic.Bool
			var lastNonce string
			var nonceMu sync.Mutex
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); w.WriteHeader(500) }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				data, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("missing authenticated no-store transport")
				}
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var q credentialIdentityRequest
					json.Unmarshal(data, &q)
					if q.AgentID != a.Expected.AgentID || q.ServerID != a.Expected.ServerID || len(q.Nonce) != 64 {
						t.Error("identity binding")
					}
					if mode == "identity_error" {
						http.Error(w, "sensitive token", 401)
						return
					}
					rp := credentialIdentityReply{1, true, q.AgentID, q.ServerID, a.AgentCredentialGeneration, recoveryReadHash(data)}
					if mode == "identity_generation" {
						rp.AgentCredentialGeneration++
					}
					if mode == "identity_hash" {
						rp.RequestHash = strings.Repeat("0", 64)
					}
					if mode == "drift_identity" {
						drift.Store(true)
					}
					if mode == "cancel_identity" {
						cancel()
					}
					json.NewEncoder(w).Encode(rp)
					return
				}
				base := "/api/v1/agent/recovery-observations/" + a.OperationID
				if r.URL.Path == base+"/prepared-release/verify" {
					if !bytes.Equal(data, pin) {
						t.Error("prepared proof changed")
					}
					if mode == "stored_missing" {
						http.Error(w, "sensitive token", 409)
						return
					}
					if mode == "stored_redirect" {
						http.Redirect(w, r, target.URL, 307)
						return
					}
					rp := RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, ReleaseIntentHash: p.ReleaseIntentHash, ProofHash: recoveryReadHash(pin), RequestHash: recoveryReadHash(pin)}
					if mode == "stored_wrong" {
						rp.ProofHash = strings.Repeat("0", 64)
					}
					if mode == "drift_stored" {
						drift.Store(true)
					}
					if mode == "cancel_stored" {
						cancel()
					}
					out, _ := json.Marshal(rp)
					if mode == "stored_duplicate" {
						out = append([]byte(`{"recorded":true,`), out[1:]...)
					}
					w.Write(out)
					return
				}
				if r.URL.Path != base+"/release-authorization" {
					t.Error("unexpected mutating/new endpoint")
					w.WriteHeader(404)
					return
				}
				var q recoveryReleaseRequest
				json.Unmarshal(data, &q)
				nonceMu.Lock()
				if q.SchemaVersion != 1 || q.OperationID != a.OperationID || q.AgentID != a.Expected.AgentID || q.ServerID != a.Expected.ServerID || q.AgentCredentialGeneration != a.AgentCredentialGeneration || len(q.Nonce) != 64 || q.Nonce == a.Nonce || q.Nonce == lastNonce || q.GrantHash != a.GrantHash || q.ConsumptionHash != a.ConsumptionHash || q.CommitProofHash != a.CommitProofHash {
					t.Error("release reread lost original binding/fresh request")
				}
				lastNonce = q.Nonce
				nonceMu.Unlock()
				if mode == "authority_revoked" || mode == "authority_missing" {
					http.Error(w, "sensitive token", 409)
					return
				}
				if mode == "authority_redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				rp := recoveryReleaseReply{1, a.OperationID, a.Expected.AgentID, a.Expected.ServerID, a.AgentCredentialGeneration, recoveryReadHash(data), recoveryReadHash(authority), a}
				switch mode {
				case "authority_hash":
					rp.AuthorizationHash = strings.Repeat("0", 64)
				case "authority_request":
					rp.RequestHash = strings.Repeat("0", 64)
				case "authority_operation":
					rp.OperationID = "foreign"
				case "authority_agent":
					rp.AgentID = "foreign"
				case "authority_server":
					rp.ServerID = "foreign"
				case "authority_generation":
					rp.AgentCredentialGeneration++
				case "authority_nonce":
					rp.Authorization.Nonce = strings.Repeat("0", 64)
				case "authority_grant":
					rp.Authorization.GrantHash = strings.Repeat("0", 64)
				case "authority_consumption":
					rp.Authorization.ConsumptionHash = strings.Repeat("0", 64)
				case "authority_commit":
					rp.Authorization.CommitProofHash = strings.Repeat("0", 64)
				case "authority_expected":
					rp.Authorization.Expected.Witness.ListenerPort++
				case "authority_expiry":
					rp.Authorization.ExpiresAt = rp.Authorization.ExpiresAt.Add(time.Second)
				case "authority_issued":
					rp.Authorization.IssuedAt = rp.Authorization.IssuedAt.Add(time.Nanosecond)
				case "drift_authority":
					drift.Store(true)
				case "cancel_authority":
					cancel()
				}
				out, _ := json.Marshal(rp)
				switch mode {
				case "authority_duplicate":
					out = append([]byte(`{"schemaVersion":1,`), out[1:]...)
				case "authority_alias":
					out = bytes.Replace(out, []byte("requestHash"), []byte("RequestHash"), 1)
				case "authority_unknown":
					out = append([]byte(`{"foreign":true,`), out[1:]...)
				case "authority_trailing":
					out = append(out, []byte("{}")...)
				case "authority_oversize":
					out = []byte(strings.Repeat(" ", 16385))
				}
				w.Write(out)
			}))
			defer s.Close()
			v := VerifiedRecoveryReleaseAuthorization{Binding: tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: s.URL, AgentID: a.Expected.AgentID, ServerID: a.Expected.ServerID}, AgentCredentialGeneration: a.AgentCredentialGeneration}, Authorization: a, AuthorizationHash: p.AuthorizationHash}
			if mode == "wrong_manager" {
				v.Binding.ManagerURL = target.URL
			}
			if mode == "wrong_vhash" {
				v.AuthorizationHash = strings.Repeat("0", 64)
			}
			refresh := func(context.Context) (tasks.RuntimeRecoveryPreparedReleaseProof, error) {
				if mode == "refresh_error" {
					return tasks.RuntimeRecoveryPreparedReleaseProof{}, errors.New("sensitive path")
				}
				out := p
				if drift.Load() {
					out.Observation.Witness.ListenerPort++
				}
				return out, nil
			}
			count := 1
			if mode == "ready" {
				count = 2
			}
			for range count {
				result, err := New(s.URL).checkRecoveryFenceReleaseReadiness(ctx, "isolated-token", v, p, refresh)
				if mode == "ready" {
					if err != nil || !result.PreparedReceipt.Recorded || result.AuthorizationHash != v.AuthorizationHash {
						t.Fatal("readiness not confirmed", err)
					}
				} else if err != ErrRecoveryReleaseNotReady || result != (RecoveryFenceReleaseReadiness{}) {
					t.Fatal("uncertainty not sanitized", err)
				}
			}
			if redirects.Load() != 0 {
				t.Fatal("redirect followed")
			}
			if mode == "ready" && calls.Load() != 6 {
				t.Fatal("missing exact repeat revalidation")
			}
			if mode != "ready" && calls.Load() > 3 {
				t.Fatal("unexpected retry or new evidence submission")
			}
			if (mode == "expired" || mode == "future" || mode == "refresh_error" || mode == "wrong_manager" || mode == "wrong_vhash") && calls.Load() != 0 {
				t.Fatal("invalid live scope reached HTTP")
			}
		})
	}
}

func TestReleaseReadinessRequiresLiveOwner(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer s.Close()
	a := recoveryFinalizationTestAuthorization(t)
	wire, _ := json.Marshal(a)
	v := VerifiedRecoveryReleaseAuthorization{Binding: tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: s.URL}}, Authorization: a, AuthorizationHash: recoveryReadHash(wire)}
	if result, err := New(s.URL).CheckRecoveryFenceReleaseReadiness(context.Background(), "isolated-token", v, nil); err != ErrRecoveryReleaseNotReady || result != (RecoveryFenceReleaseReadiness{}) || calls.Load() != 0 {
		t.Fatal("historical value bypassed live owner", err)
	}
}
