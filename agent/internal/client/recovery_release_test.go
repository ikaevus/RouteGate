package client

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ikaevus/routegate/agent/internal/tasks"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeMutationRecoveryReleaseTransport(t *testing.T) {
	for _, mode := range []string{"valid", "with_ack", "identity_generation", "wrong_request", "wrong_hash", "wrong_operation", "wrong_generation", "wrong_grant", "wrong_consumption", "wrong_proof", "expired", "future", "duplicate", "alias", "unknown_field", "trailing", "oversize", "redirect", "changed_during_http", "cancelled", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			g := recoveryReadTestGrant(t)
			g.IssuedAt = time.Now().UTC().Add(-time.Hour)
			g.ExpiresAt = g.IssuedAt.Add(time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dir string
			var pinned []byte
			reads, redirects := 0, 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; w.WriteHeader(500) }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("auth/no-store")
				}
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var req credentialIdentityRequest
					json.Unmarshal(data, &req)
					gen := int64(7)
					if mode == "identity_generation" {
						gen++
					}
					json.NewEncoder(w).Encode(credentialIdentityReply{1, true, req.AgentID, req.ServerID, gen, recoveryReadHash(data)})
					return
				}
				reads++
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/release-authorization" {
					t.Error("wrong endpoint")
				}
				var req recoveryReleaseRequest
				var p tasks.RuntimeRecoveryCommitProof
				json.Unmarshal(data, &req)
				json.Unmarshal(pinned, &p)
				if req.OperationID != p.OperationID || len(req.Nonce) != 64 || req.GrantHash != p.GrantHash || req.ConsumptionHash != p.ConsumptionHash || req.CommitProofHash != recoveryReadHash(pinned) {
					t.Error("lost exact historical bindings")
				}
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				if mode == "unauthorized" {
					http.Error(w, "secret-token", 401)
					return
				}
				now := time.Now().UTC()
				a := tasks.RuntimeRecoveryReleaseAuthorization{SchemaVersion: 1, Purpose: "runtime_mutation_fence_final_release_v1", OperationID: p.OperationID, Nonce: strings.Repeat("6", 64), AgentCredentialGeneration: 7, GrantHash: p.GrantHash, ConsumptionHash: p.ConsumptionHash, CommitProofHash: recoveryReadHash(pinned), IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Expected: p.Observation}
				switch mode {
				case "wrong_operation":
					a.OperationID = g.Expected.TaskID
				case "wrong_generation":
					a.AgentCredentialGeneration++
				case "wrong_grant":
					a.GrantHash = strings.Repeat("0", 64)
				case "wrong_consumption":
					a.ConsumptionHash = strings.Repeat("0", 64)
				case "wrong_proof":
					a.CommitProofHash = strings.Repeat("0", 64)
				case "expired":
					a.IssuedAt = now.Add(-2 * time.Minute)
					a.ExpiresAt = now.Add(-time.Second)
				case "future":
					a.IssuedAt = now.Add(time.Second)
				case "changed_during_http":
					os.WriteFile(filepath.Join(dir, "recovery-submission.json"), []byte(`{}`), 0600)
				case "cancelled":
					cancel()
				}
				authWire, _ := json.Marshal(a)
				reply := recoveryReleaseReply{1, p.OperationID, g.Expected.AgentID, g.Expected.ServerID, 7, recoveryReadHash(data), recoveryReadHash(authWire), a}
				if mode == "wrong_request" {
					reply.RequestHash = strings.Repeat("0", 64)
				}
				if mode == "wrong_hash" {
					reply.AuthorizationHash = strings.Repeat("0", 64)
				}
				out, _ := json.Marshal(reply)
				if mode == "duplicate" {
					out = append([]byte(`{"schemaVersion":1,`), out[1:]...)
				}
				if mode == "alias" {
					out = bytes.Replace(out, []byte(`"schemaVersion"`), []byte(`"SchemaVersion"`), 1)
				}
				if mode == "unknown_field" {
					out = append([]byte(`{"secret":"payload",`), out[1:]...)
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
			out, err := c.FetchRecoveryReleaseAuthorization(ctx, "isolated-token", b.AgentID, b.ServerID, dir)
			valid := mode == "valid" || mode == "with_ack"
			if valid {
				if err != nil || out.AuthorizationHash == "" {
					t.Fatal("valid delivery", err)
				}
			} else if err != errRecoveryGrantRead || out != (VerifiedRecoveryReleaseAuthorization{}) {
				t.Fatal("unsafe/unsanitized delivery", err)
			}
			if redirects != 0 {
				t.Fatal("followed redirect")
			}
			if mode == "identity_generation" {
				if reads != 0 {
					t.Fatal("rotated identity reached delivery")
				}
			} else if reads != 1 {
				t.Fatal("unexpected retries", reads)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != len(files) {
				t.Fatal("created/deleted state")
			}
			for name, want := range files {
				if mode == "changed_during_http" && name == "recovery-submission.json" {
					continue
				}
				got, e := os.ReadFile(filepath.Join(dir, name))
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("delivery changed durable state", name)
				}
			}
		})
	}
}
