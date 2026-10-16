package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func writePreparedReleaseHistoryFixture(t *testing.T, b tasks.RuntimeRecoveryGrantBinding, g tasks.RuntimeRecoveryMutationGrant) (string, []byte) {
	t.Helper()
	dir, finalWire, files := writeRecoveryFinalHistoryFixture(t, b, g, "terminal")
	var final tasks.RuntimeRecoveryFinalizationProof
	var terminal struct {
		IntentHash, FinalProofHash, FinalReceiptHash string
		ObservedAt                                   time.Time
		Observation                                  tasks.RuntimeRecoveryObservation
	}
	if json.Unmarshal(finalWire, &final) != nil || json.Unmarshal(files["recovery-retirement-terminal.json"], &terminal) != nil {
		t.Fatal("fixture chain")
	}
	p := tasks.RuntimeRecoveryTerminalProof{SchemaVersion: 1, Purpose: "runtime_mutation_retirement_evidence_v1", OperationID: final.OperationID, AgentCredentialGeneration: b.AgentCredentialGeneration, AuthorizationHash: final.AuthorizationHash, FinalConsumptionHash: final.FinalConsumptionHash, FinalProofHash: terminal.FinalProofHash, FinalReceiptHash: terminal.FinalReceiptHash, IntentHash: terminal.IntentHash, TerminalHash: recoveryReadHash(files["recovery-retirement-terminal.json"]), ObservedAt: terminal.ObservedAt, Observation: terminal.Observation}
	pin, _ := json.Marshal(p)
	ack, _ := json.Marshal(RecoveryTerminalReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, TerminalHash: p.TerminalHash, ProofHash: recoveryReadHash(pin), RequestHash: recoveryReadHash(pin)})
	files["recovery-terminal-submission.json"], files["recovery-terminal-acknowledgement.json"] = pin, ack
	data, err := os.ReadFile(filepath.Join(dir, "retired-"+p.OperationID, "recovery-final-consumption.json"))
	if err != nil {
		t.Fatal(err)
	}
	var consumed struct {
		Authorization tasks.RuntimeRecoveryReleaseAuthorization
	}
	if json.Unmarshal(data, &consumed) != nil {
		t.Fatal("fixture expiry")
	}
	type entry struct {
		Name string `json:"name"`
		Hash string `json:"hash"`
	}
	r := struct {
		SchemaVersion       int                               `json:"schemaVersion"`
		Purpose             string                            `json:"purpose"`
		OperationID         string                            `json:"operationId"`
		Binding             tasks.RuntimeRecoveryGrantBinding `json:"binding"`
		AuthorizationHash   string                            `json:"authorizationHash"`
		TerminalProofHash   string                            `json:"terminalProofHash"`
		TerminalReceiptHash string                            `json:"terminalReceiptHash"`
		PreparedAt          time.Time                         `json:"preparedAt"`
		ExpiresAt           time.Time                         `json:"expiresAt"`
		ArchiveName         string                            `json:"archiveName"`
		Files               [5]entry                          `json:"files"`
	}{SchemaVersion: 1, Purpose: "runtime_mutation_fence_release_prepared_v1", OperationID: p.OperationID, Binding: b, AuthorizationHash: p.AuthorizationHash, TerminalProofHash: recoveryReadHash(pin), TerminalReceiptHash: recoveryReadHash(ack), PreparedAt: p.ObservedAt.Add(time.Second), ExpiresAt: consumed.Authorization.ExpiresAt, ArchiveName: "retired-" + p.OperationID}
	for i, name := range []string{"recovery-retirement-intent.json", "recovery-retirement-terminal.json", "recovery-terminal-submission.json", "recovery-terminal-acknowledgement.json", "mutation-inflight.json"} {
		r.Files[i] = entry{name, recoveryReadHash(files[name])}
	}
	intent, _ := json.Marshal(r)
	files["recovery-fence-release-intent.json"] = intent
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, pin
}

func preparedHistoryTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	if err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if e.IsDir() {
			m[rel+"/"] = nil
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		m[rel] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRuntimeMutationPreparedReleaseReconciliationTransport(t *testing.T) {
	for _, mode := range []string{"expired", "rotated", "identity_hash", "identity_error", "absent", "unauthorized", "redirect", "wrong_operation", "wrong_terminal", "wrong_proof", "wrong_request", "wrong_schema", "not_recorded", "duplicate", "alias", "unknown_field", "trailing", "oversize", "missing_intent", "missing_ack", "bad_intent", "intent_hardlink", "intent_changed", "same_bytes_replaced", "pin_changed", "ack_changed", "archive_changed", "unknown_created", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			g := recoveryReadTestGrant(t)
			g.IssuedAt = time.Now().UTC().Add(-time.Hour)
			g.ExpiresAt = g.IssuedAt.Add(time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dir string
			var pin []byte
			var b tasks.RuntimeRecoveryGrantBinding
			calls, reads, redirects := 0, 0, 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; w.WriteHeader(500) }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("unauthenticated readback")
				}
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var req credentialIdentityRequest
					json.Unmarshal(body, &req)
					if len(req.Nonce) != 64 || req.AgentID != g.Expected.AgentID || req.ServerID != g.Expected.ServerID {
						t.Error("unbound identity")
					}
					reply := credentialIdentityReply{1, true, req.AgentID, req.ServerID, 7, recoveryReadHash(body)}
					if mode == "rotated" {
						reply.AgentCredentialGeneration++
					}
					if mode == "identity_hash" {
						reply.RequestHash = strings.Repeat("0", 64)
					}
					if mode == "identity_error" {
						http.Error(w, "secret-token", 401)
						return
					}
					json.NewEncoder(w).Encode(reply)
					return
				}
				reads++
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/terminal/verify" || !bytes.Equal(body, pin) {
					t.Error("request was not exact historical terminal readback")
				}
				if tasks.WithHistoricalRecoveryPreparedRelease(ctx, dir, b, func(tasks.RuntimeRecoveryPreparedReleaseHistory) error { t.Error("HTTP lost flock"); return nil }) != tasks.ErrRuntimeMutationBlocked {
					t.Error("HTTP snapshot did not retain flock")
				}
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				if mode == "absent" || mode == "unauthorized" {
					status := 409
					if mode == "unauthorized" {
						status = 401
					}
					http.Error(w, "secret-token", status)
					return
				}
				var p tasks.RuntimeRecoveryTerminalProof
				json.Unmarshal(body, &p)
				reply := RecoveryTerminalReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, TerminalHash: p.TerminalHash, ProofHash: recoveryReadHash(body), RequestHash: recoveryReadHash(body)}
				write := func(name string, data []byte) {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Error(err)
					}
				}
				switch mode {
				case "wrong_operation":
					reply.OperationID = "foreign"
				case "wrong_terminal":
					reply.TerminalHash = strings.Repeat("0", 64)
				case "wrong_proof":
					reply.ProofHash = strings.Repeat("0", 64)
				case "wrong_request":
					reply.RequestHash = strings.Repeat("0", 64)
				case "wrong_schema":
					reply.SchemaVersion = 2
				case "not_recorded":
					reply.Recorded = false
				case "intent_changed":
					write("recovery-fence-release-intent.json", []byte("{}"))
				case "same_bytes_replaced":
					data, err := os.ReadFile(filepath.Join(dir, "recovery-fence-release-intent.json"))
					if err != nil {
						t.Error(err)
					}
					write(".replacement", data)
					if err := os.Rename(filepath.Join(dir, ".replacement"), filepath.Join(dir, "recovery-fence-release-intent.json")); err != nil {
						t.Error(err)
					}
				case "pin_changed":
					write("recovery-terminal-submission.json", []byte("{}"))
				case "ack_changed":
					write("recovery-terminal-acknowledgement.json", []byte("{}"))
				case "archive_changed":
					write(filepath.Join("retired-"+g.OperationID, "recovery-final-submission.json"), []byte("{}"))
				case "unknown_created":
					write("unknown.json", []byte("{}"))
				case "cancelled":
					cancel()
				}
				out, _ := json.Marshal(reply)
				switch mode {
				case "duplicate":
					out = append([]byte(`{"recorded":true,`), out[1:]...)
				case "alias":
					out = bytes.Replace(out, []byte("proofHash"), []byte("ProofHash"), 1)
				case "unknown_field":
					out = append([]byte(`{"secret":"token",`), out[1:]...)
				case "trailing":
					out = append(out, []byte("{}")...)
				case "oversize":
					out = []byte(strings.Repeat(" ", 16385))
				}
				w.Write(out)
			}))
			defer s.Close()
			b = tasks.RuntimeRecoveryGrantBinding{RuntimeResultBinding: tasks.RuntimeResultBinding{ManagerURL: s.URL, AgentID: g.Expected.AgentID, ServerID: g.Expected.ServerID}, AgentCredentialGeneration: 7}
			dir, pin = writePreparedReleaseHistoryFixture(t, b, g)
			intentPath := filepath.Join(dir, "recovery-fence-release-intent.json")
			intent, err := os.ReadFile(intentPath)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing_intent":
				os.Remove(intentPath)
			case "missing_ack":
				os.Remove(filepath.Join(dir, "recovery-terminal-acknowledgement.json"))
			case "bad_intent":
				os.WriteFile(intentPath, []byte("{}"), 0600)
			case "intent_hardlink":
				if err := os.Link(intentPath, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			}
			before := preparedHistoryTree(t, dir)
			result, err := New(s.URL).ReconcileRecoveryPreparedRelease(ctx, "isolated-token", b.AgentID, b.ServerID, dir)
			if mode == "expired" {
				if err != nil || !result.TerminalReceipt.Recorded || result.ReleaseIntentHash != recoveryReadHash(intent) || !result.AuthorizationExpiresAt.Before(time.Now()) || result.PreparedAt.Before(g.IssuedAt) {
					t.Fatal("expired history unconfirmed/renewed", err)
				}
			} else if err != ErrRecoveryTerminalUnconfirmed || result != (RecoveryPreparedReleaseReadback{}) {
				t.Fatal("uncertainty not sanitized", err)
			}
			if redirects != 0 {
				t.Fatal("redirect followed")
			}
			invalidLocal := mode == "rotated" || mode == "identity_hash" || mode == "identity_error" || mode == "missing_intent" || mode == "missing_ack" || mode == "bad_intent" || mode == "intent_hardlink"
			if invalidLocal {
				if reads != 0 {
					t.Fatal("invalid identity/local history reached readback")
				}
			} else if calls != 2 || reads != 1 {
				t.Fatal("unexpected new authority/retries", calls, reads)
			}
			switch mode {
			case "intent_changed":
				before["recovery-fence-release-intent.json"] = []byte("{}")
			case "pin_changed":
				before["recovery-terminal-submission.json"] = []byte("{}")
			case "ack_changed":
				before["recovery-terminal-acknowledgement.json"] = []byte("{}")
			case "archive_changed":
				before[filepath.Join("retired-"+g.OperationID, "recovery-final-submission.json")] = []byte("{}")
			case "unknown_created":
				before["unknown.json"] = []byte("{}")
			}
			after := preparedHistoryTree(t, dir)
			if len(before) != len(after) {
				t.Fatal("readback created/deleted state")
			}
			for name, data := range before {
				got, ok := after[name]
				if !ok || !bytes.Equal(data, got) {
					t.Fatal("readback changed/repaired history", name)
				}
			}
		})
	}
}
