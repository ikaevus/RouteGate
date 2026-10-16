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

func TestRuntimeMutationTerminalRecoveryReconciliationTransport(t *testing.T) {
	for _, mode := range []string{"expired", "with_ack", "terminal", "partial_terminal", "rotated", "identity_hash", "identity_error", "unknown", "absent", "unauthorized", "wrong_operation", "wrong_terminal", "wrong_proof", "wrong_request", "wrong_schema", "not_recorded", "duplicate", "alias", "unknown_field", "trailing", "oversize", "redirect", "changed_during_http", "terminal_changed_during_http", "archive_changed_during_http", "ack_created_during_http", "cancelled", "missing_pin", "pin_hardlink", "ack_invalid"} {
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
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/terminal/verify" || !bytes.Equal(data, pinned) {
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
				var p tasks.RuntimeRecoveryTerminalProof
				json.Unmarshal(data, &p)
				receipt := RecoveryTerminalReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, TerminalHash: p.TerminalHash, ProofHash: recoveryReadHash(data), RequestHash: recoveryReadHash(data)}
				switch mode {
				case "wrong_operation":
					receipt.OperationID = "foreign"
				case "wrong_terminal":
					receipt.TerminalHash = strings.Repeat("0", 64)
				case "wrong_proof":
					receipt.ProofHash = strings.Repeat("0", 64)
				case "wrong_request":
					receipt.RequestHash = strings.Repeat("0", 64)
				case "wrong_schema":
					receipt.SchemaVersion = 2
				case "not_recorded":
					receipt.Recorded = false
				case "changed_during_http":
					os.WriteFile(filepath.Join(dir, "recovery-terminal-submission.json"), []byte(`{}`), 0600)
				case "terminal_changed_during_http":
					os.WriteFile(filepath.Join(dir, "recovery-retirement-terminal.json"), []byte("{}"), 0600)
				case "archive_changed_during_http":
					os.WriteFile(filepath.Join(dir, "retired-"+g.OperationID, "recovery-final-submission.json"), []byte("{}"), 0600)
				case "ack_created_during_http":
					os.WriteFile(filepath.Join(dir, "recovery-terminal-acknowledgement.json"), []byte("{}"), 0600)
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
				if mode == "alias" {
					out = bytes.Replace(out, []byte("proofHash"), []byte("ProofHash"), 1)
				}
				if mode == "unknown_field" {
					out = append([]byte(`{"untrusted":"secret",`), out[1:]...)
				}
				if mode == "oversize" {
					out = []byte(strings.Repeat(" ", 16385))
				}
				w.Write(out)
			}))
			defer s.Close()
			b := tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: s.URL, AgentID: g.Expected.AgentID, ServerID: g.Expected.ServerID}, AgentCredentialGeneration: 7}
			fixtureMode := "terminal"
			if mode == "partial_terminal" {
				fixtureMode = mode
			}
			dir, pinned, files := writeRecoveryFinalHistoryFixture(t, b, g, fixtureMode)
			var final tasks.RuntimeRecoveryFinalizationProof
			if json.Unmarshal(pinned, &final) != nil {
				t.Fatal("final fixture")
			}
			var terminal struct {
				IntentHash, FinalProofHash, FinalReceiptHash string
				ObservedAt                                   time.Time
				Observation                                  tasks.RuntimeRecoveryObservation
			}
			terminalData := files["recovery-retirement-terminal.json"]
			if json.Unmarshal(terminalData, &terminal) != nil {
				t.Fatal("terminal fixture")
			}
			p := tasks.RuntimeRecoveryTerminalProof{SchemaVersion: 1, Purpose: "runtime_mutation_retirement_evidence_v1", OperationID: final.OperationID, AgentCredentialGeneration: b.AgentCredentialGeneration, AuthorizationHash: final.AuthorizationHash, FinalConsumptionHash: final.FinalConsumptionHash, FinalProofHash: terminal.FinalProofHash, FinalReceiptHash: terminal.FinalReceiptHash, IntentHash: terminal.IntentHash, TerminalHash: recoveryReadHash(terminalData), ObservedAt: terminal.ObservedAt, Observation: terminal.Observation}
			pinned, _ = json.Marshal(p)
			files["recovery-terminal-submission.json"] = pinned
			if mode == "with_ack" {
				files["recovery-terminal-acknowledgement.json"], _ = json.Marshal(RecoveryTerminalReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, TerminalHash: p.TerminalHash, ProofHash: recoveryReadHash(pinned), RequestHash: recoveryReadHash(pinned)})
			}
			for _, name := range []string{"recovery-terminal-submission.json", "recovery-terminal-acknowledgement.json"} {
				if data, ok := files[name]; ok {
					if os.WriteFile(filepath.Join(dir, name), data, 0600) != nil {
						t.Fatal("terminal pin fixture")
					}
				}
			}
			c := New(s.URL)
			switch mode {
			case "missing_pin":
				os.Remove(filepath.Join(dir, "recovery-terminal-submission.json"))
				delete(files, "recovery-terminal-submission.json")
			case "pin_hardlink":
				os.Link(filepath.Join(dir, "recovery-terminal-submission.json"), filepath.Join(dir, "pin-alias"))
			case "ack_invalid":
				files["recovery-terminal-acknowledgement.json"] = []byte("{}")
				os.WriteFile(filepath.Join(dir, "recovery-terminal-acknowledgement.json"), []byte("{}"), 0600)
			}
			r, err := c.ReconcileRecoveryTerminal(ctx, "isolated-token", b.AgentID, b.ServerID, dir)
			valid := mode == "expired" || mode == "with_ack" || mode == "terminal" || mode == "partial_terminal"
			if valid {
				if err != nil || !r.Recorded {
					t.Fatal("historical read failed", err)
				}
			} else if err != ErrRecoveryTerminalUnconfirmed || r != (RecoveryTerminalReceipt{}) {
				t.Fatal("uncertainty not sanitized", err)
			}
			if redirects != 0 {
				t.Fatal("redirect followed")
			}
			if mode == "rotated" || mode == "identity_hash" || mode == "identity_error" || mode == "missing_pin" || mode == "pin_hardlink" || mode == "ack_invalid" {
				if reads != 0 {
					t.Fatal("bad current identity reached readback")
				}
			} else if reads != 1 || calls != 2 {
				t.Fatal("unexpected retries/new authority", calls, reads)
			}
			entries, _ := os.ReadDir(dir)
			expectedEntries := len(files) + 1
			if mode == "pin_hardlink" || mode == "ack_created_during_http" {
				expectedEntries++
			}
			if len(entries) != expectedEntries {
				t.Fatal("reconciliation created/deleted state")
			}
			injected := ""
			switch mode {
			case "changed_during_http":
				injected = filepath.Join(dir, "recovery-terminal-submission.json")
			case "terminal_changed_during_http":
				injected = filepath.Join(dir, "recovery-retirement-terminal.json")
			case "archive_changed_during_http":
				injected = filepath.Join(dir, "retired-"+g.OperationID, "recovery-final-submission.json")
			case "ack_created_during_http":
				injected = filepath.Join(dir, "recovery-terminal-acknowledgement.json")
			}
			if injected != "" {
				got, e := os.ReadFile(injected)
				if e != nil || string(got) != "{}" {
					t.Fatal("readback repaired/removed uncertain state", e)
				}
			}
			for name, want := range files {
				if mode == "changed_during_http" && name == "recovery-terminal-submission.json" {
					continue
				}
				if mode == "terminal_changed_during_http" && name == "recovery-retirement-terminal.json" {
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
