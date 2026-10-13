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
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestRuntimeMutationRecoveryReconciliationTransport(t *testing.T) {
	for _, mode := range []string{"expired", "with_ack", "rotated", "identity_hash", "identity_error", "unknown", "absent", "unauthorized", "wrong_operation", "wrong_grant", "wrong_consumption", "wrong_proof", "wrong_request", "not_recorded", "duplicate", "trailing", "oversize", "redirect", "changed_during_http", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			g := recoveryReadTestGrant(t)
			// Historical timestamps deliberately expired: no release is attempted.
			g.IssuedAt = time.Now().UTC().Add(-time.Hour)
			g.ExpiresAt = g.IssuedAt.Add(time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var pinned []byte
			var dir string
			calls, reads, redirects := 0, 0, 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; w.WriteHeader(500) }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("missing authenticated transport")
				}
				data, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var req credentialIdentityRequest
					json.Unmarshal(data, &req)
					if len(req.Nonce) != 64 || req.AgentID != g.Expected.AgentID || req.ServerID != g.Expected.ServerID {
						t.Error("identity not bound")
					}
					r := credentialIdentityReply{1, true, req.AgentID, req.ServerID, 7, recoveryReadHash(data)}
					if mode == "rotated" {
						r.AgentCredentialGeneration++
					}
					if mode == "identity_hash" {
						r.RequestHash = strings.Repeat("0", 64)
					}
					if mode == "identity_error" {
						http.Error(w, "secret-token", 401)
						return
					}
					json.NewEncoder(w).Encode(r)
					return
				}
				reads++
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/commit/verify" || !bytes.Equal(data, pinned) {
					t.Error("new authority/request bytes instead of historical read")
				}
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				if mode == "unknown" || mode == "absent" || mode == "unauthorized" {
					status := 500
					if mode == "absent" {
						status = 409
					}
					if mode == "unauthorized" {
						status = 401
					}
					http.Error(w, "secret-token", status)
					return
				}
				var p tasks.RuntimeRecoveryCommitProof
				json.Unmarshal(data, &p)
				receipt := RecoveryCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, GrantHash: p.GrantHash, ConsumptionHash: p.ConsumptionHash, ProofHash: recoveryReadHash(data), RequestHash: recoveryReadHash(data)}
				switch mode {
				case "wrong_operation":
					receipt.OperationID = "foreign"
				case "wrong_grant":
					receipt.GrantHash = strings.Repeat("0", 64)
				case "wrong_consumption":
					receipt.ConsumptionHash = strings.Repeat("0", 64)
				case "wrong_proof":
					receipt.ProofHash = strings.Repeat("0", 64)
				case "wrong_request":
					receipt.RequestHash = strings.Repeat("0", 64)
				case "not_recorded":
					receipt.Recorded = false
				case "changed_during_http":
					os.WriteFile(filepath.Join(dir, "recovery-submission.json"), []byte(`{}`), 0600)
				case "cancelled":
					cancel()
				}
				out, _ := json.Marshal(receipt)
				if mode == "duplicate" {
					out = append([]byte(`{"recorded":true,`), out[1:]...)
				}
				if mode == "trailing" {
					out = append(out, []byte(`{}`)...)
				}
				if mode == "oversize" {
					out = []byte(strings.Repeat(" ", 16385))
				}
				w.Write(out)
			}))
			defer s.Close()
			b := tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: s.URL, AgentID: g.Expected.AgentID, ServerID: g.Expected.ServerID}, AgentCredentialGeneration: 7}
			dir = t.TempDir()
			os.Chmod(dir, 0700)
			marker, _ := json.Marshal(struct {
				SchemaVersion int    `json:"schemaVersion"`
				TaskID        string `json:"taskId"`
				Kind          string `json:"kind"`
			}{1, g.Expected.TaskID, tasks.TaskKindVPNCoreService})
			envelope := json.RawMessage(`{"status":"succeeded"}`)
			g.Expected.MarkerHash = recoveryReadHash(marker)
			g.Expected.EnvelopeHash = recoveryReadHash(envelope)
			taskReceipt, _ := json.Marshal(struct {
				SchemaVersion  int                           `json:"schemaVersion"`
				Binding        tasks.RuntimeResultBinding    `json:"binding"`
				MarkerHash     string                        `json:"markerHash"`
				TaskID         string                        `json:"taskId"`
				Envelope       json.RawMessage               `json:"envelope"`
				Acknowledged   bool                          `json:"acknowledged"`
				RuntimeWitness *tasks.RuntimeRecoveryWitness `json:"runtimeWitness,omitempty"`
			}{1, b.RuntimeResultBinding, g.Expected.MarkerHash, g.Expected.TaskID, envelope, true, &g.Expected.Witness})
			g.Expected.ReceiptHash = recoveryReadHash(taskReceipt)
			grantWire, _ := json.Marshal(g)
			consumption, _ := json.Marshal(struct {
				SchemaVersion int                                `json:"schemaVersion"`
				Binding       tasks.RuntimeRecoveryGrantBinding  `json:"binding"`
				Grant         tasks.RuntimeRecoveryMutationGrant `json:"grant"`
				GrantHash     string                             `json:"grantHash"`
				ObservedAt    time.Time                          `json:"observedAt"`
				Observation   tasks.RuntimeRecoveryObservation   `json:"observation"`
			}{1, b, g, recoveryReadHash(grantWire), g.IssuedAt.Add(time.Second), g.Expected})
			p := tasks.RuntimeRecoveryCommitProof{SchemaVersion: 1, OperationID: g.OperationID, GrantHash: recoveryReadHash(grantWire), ConsumptionHash: recoveryReadHash(consumption), ObservedAt: g.IssuedAt.Add(2 * time.Second), Observation: g.Expected}
			pinned, _ = json.Marshal(p)
			files := map[string][]byte{"runtime.lock": {}, "mutation-inflight.json": marker, "mutation-result.json": taskReceipt, "recovery-consumption.json": consumption, "recovery-submission.json": pinned}
			if mode == "with_ack" {
				files["recovery-acknowledgement.json"], _ = json.Marshal(RecoveryCommitReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, GrantHash: p.GrantHash, ConsumptionHash: p.ConsumptionHash, ProofHash: recoveryReadHash(pinned), RequestHash: recoveryReadHash(pinned)})
			}
			for name, data := range files {
				if os.WriteFile(filepath.Join(dir, name), data, 0600) != nil {
					t.Fatal("fixture write")
				}
			}
			c := New(s.URL)
			r, err := c.ReconcileRecoveryCommit(ctx, "isolated-token", b.AgentID, b.ServerID, dir)
			valid := mode == "expired" || mode == "with_ack"
			if valid {
				if err != nil || !r.Recorded {
					t.Fatal("historical read failed", err)
				}
			} else if err != ErrRecoveryCommitUnconfirmed || r != (RecoveryCommitReceipt{}) {
				t.Fatal("uncertainty not sanitized", err)
			}
			if redirects != 0 {
				t.Fatal("redirect followed")
			}
			if mode == "rotated" || mode == "identity_hash" || mode == "identity_error" {
				if reads != 0 {
					t.Fatal("bad current identity reached readback")
				}
			} else if reads != 1 || calls != 2 {
				t.Fatal("unexpected retries/new authority", calls, reads)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != len(files) {
				t.Fatal("reconciliation created/deleted state")
			}
			for name, want := range files {
				if mode == "changed_during_http" && name == "recovery-submission.json" {
					continue
				}
				got, e := os.ReadFile(filepath.Join(dir, name))
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("reconciliation changed state", name)
				}
			}
		})
	}
}
