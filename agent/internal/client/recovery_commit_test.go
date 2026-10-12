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

func TestRuntimeMutationRecoveryCommitTransport(t *testing.T) {
	for _, mode := range []string{"accepted", "lost_ack", "lost_connection", "retry_absent", "rejected_but_recorded", "unknown", "rejected", "unauthorized", "wrong_hash", "wrong_request", "wrong_grant", "wrong_consumption", "wrong_operation", "wrong_schema", "not_recorded", "duplicate", "alias", "unknown_field", "trailing", "oversize", "redirect", "drift_after_ack", "cancelled_after_ack", "refresh_failed"} {
		t.Run(mode, func(t *testing.T) {
			g := recoveryReadTestGrant(t)
			grantWire, _ := json.Marshal(g)
			frozen := tasks.RuntimeRecoveryCommitProof{SchemaVersion: 1, OperationID: g.OperationID, GrantHash: recoveryReadHash(grantWire), ConsumptionHash: strings.Repeat("9", 64), ObservedAt: time.Now().UTC(), Observation: g.Expected}
			var calls, posts, redirects atomic.Int32
			var drift atomic.Bool
			var mu sync.Mutex
			var bodies [][]byte
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1); w.WriteHeader(500) }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				data, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, append([]byte(nil), data...))
				mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer rg_agent_isolated" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("missing auth/no-store")
				}
				verify := strings.HasSuffix(r.URL.Path, "/verify")
				if !verify {
					posts.Add(1)
				}
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/commit" && r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/commit/verify" {
					t.Error("wrong endpoint")
				}
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				if mode == "unknown" {
					http.Error(w, "secret-token-nonce", 500)
					return
				}
				if mode == "rejected" {
					http.Error(w, "secret-token-nonce", 409)
					return
				}
				if mode == "unauthorized" {
					http.Error(w, "secret-token-nonce", 401)
					return
				}
				if !verify && (mode == "lost_ack" || mode == "retry_absent" && posts.Load() == 1) {
					http.Error(w, "secret-token-nonce", 500)
					return
				}
				if !verify && mode == "lost_connection" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
					} else {
						conn.Close()
					}
					return
				}
				if !verify && mode == "rejected_but_recorded" {
					http.Error(w, "secret-token-nonce", 409)
					return
				}
				if verify && mode == "retry_absent" {
					http.Error(w, "absent", 409)
					return
				}
				// Invalid acknowledgements are followed by definitive readback denial.
				bad := strings.HasPrefix(mode, "wrong_") || mode == "not_recorded" || mode == "duplicate" || mode == "alias" || mode == "unknown_field" || mode == "trailing" || mode == "oversize"
				if verify && bad {
					http.Error(w, "denied", 401)
					return
				}
				rp := RecoveryCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: frozen.OperationID, GrantHash: frozen.GrantHash, ConsumptionHash: frozen.ConsumptionHash, ProofHash: recoveryReadHash(data), RequestHash: recoveryReadHash(data)}
				switch mode {
				case "wrong_hash":
					rp.ProofHash = strings.Repeat("a", 64)
				case "wrong_request":
					rp.RequestHash = strings.Repeat("a", 64)
				case "wrong_grant":
					rp.GrantHash = strings.Repeat("a", 64)
				case "wrong_consumption":
					rp.ConsumptionHash = strings.Repeat("a", 64)
				case "wrong_operation":
					rp.OperationID = g.Expected.AgentID
				case "wrong_schema":
					rp.SchemaVersion = 2
				case "not_recorded":
					rp.Recorded = false
				case "drift_after_ack":
					drift.Store(true)
				case "cancelled_after_ack":
					cancel()
				}
				wire, _ := json.Marshal(rp)
				switch mode {
				case "duplicate":
					wire = append([]byte(`{"schemaVersion":1,`), wire[1:]...)
				case "alias":
					wire = []byte(strings.Replace(string(wire), "proofHash", "ProofHash", 1))
				case "unknown_field":
					wire = append([]byte(`{"unknown":true,`), wire[1:]...)
				case "trailing":
					wire = append(wire, []byte(`{}`)...)
				case "oversize":
					wire = append(wire, []byte(strings.Repeat(" ", 16384))...)
				}
				w.Write(wire)
			}))
			defer s.Close()
			c := New(s.URL)
			v := VerifiedRecoveryGrant{Binding: tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: s.URL, AgentID: g.Expected.AgentID, ServerID: g.Expected.ServerID}, AgentCredentialGeneration: g.AgentCredentialGeneration}, Grant: g, GrantHash: frozen.GrantHash}
			refresh := func(context.Context) (tasks.RuntimeRecoveryCommitProof, error) {
				p := frozen
				p.ObservedAt = time.Now().UTC()
				if mode == "refresh_failed" {
					return tasks.RuntimeRecoveryCommitProof{}, errors.New("secret-local-path")
				}
				if drift.Load() {
					p.Observation.Witness.ListenerPort++
				}
				return p, nil
			}
			result, err := c.submitRecoveryCommit(ctx, "rg_agent_isolated", v, frozen, refresh)
			want := mode == "accepted" || mode == "lost_ack" || mode == "lost_connection" || mode == "retry_absent" || mode == "rejected_but_recorded"
			if want {
				if err != nil || !result.Recorded || result.ProofHash != result.RequestHash || result.GrantHash != frozen.GrantHash {
					t.Fatal("storage not confirmed", err)
				}
			} else if err != ErrRecoveryCommitUnconfirmed || result != (RecoveryCommitReceipt{}) {
				t.Fatal("unsafe confirmation", err)
			}
			if redirects.Load() != 0 {
				t.Fatal("redirect followed")
			}
			if mode == "refresh_failed" && calls.Load() != 0 {
				t.Fatal("invalid owner reached HTTP")
			}
			if mode == "unknown" && (posts.Load() != completeTaskMaxAttempts || calls.Load() != 2*completeTaskMaxAttempts) {
				t.Fatal("uncertainty did not use bounded exact retries")
			}
			if mode == "retry_absent" && posts.Load() != 2 {
				t.Fatal("absent readback did not retry original proof")
			}
			wire, _ := json.Marshal(frozen)
			mu.Lock()
			defer mu.Unlock()
			for _, body := range bodies {
				if !bytes.Equal(body, wire) {
					t.Fatal("retry/readback changed frozen observed time or payload")
				}
			}
		})
	}
}

func TestRuntimeMutationRecoveryCommitTransportRequiresOwner(t *testing.T) {
	g := recoveryReadTestGrant(t)
	wire, _ := json.Marshal(g)
	c := New("https://isolated.invalid")
	v := VerifiedRecoveryGrant{Binding: tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: c.managerURL, AgentID: g.Expected.AgentID, ServerID: g.Expected.ServerID}, AgentCredentialGeneration: g.AgentCredentialGeneration}, Grant: g, GrantHash: recoveryReadHash(wire)}
	if out, err := c.SubmitRecoveryCommit(context.Background(), "rg_agent_isolated", v, nil); err != ErrRecoveryCommitUnconfirmed || out.Recorded {
		t.Fatal("nil owner accepted", err)
	}
}
