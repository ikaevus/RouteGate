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
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func writePreparedSubmissionHistoryFixture(t *testing.T, b tasks.RuntimeRecoveryGrantBinding, g tasks.RuntimeRecoveryMutationGrant, acknowledged bool) (string, []byte) {
	t.Helper()
	dir, terminalWire := writePreparedReleaseHistoryFixture(t, b, g)
	var terminal tasks.RuntimeRecoveryTerminalProof
	if err := json.Unmarshal(terminalWire, &terminal); err != nil {
		t.Fatal(err)
	}
	intent, err := os.ReadFile(filepath.Join(dir, "recovery-fence-release-intent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		PreparedAt          time.Time
		TerminalReceiptHash string
	}
	if err := json.Unmarshal(intent, &r); err != nil {
		t.Fatal(err)
	}
	p := tasks.RuntimeRecoveryPreparedReleaseProof{SchemaVersion: 1, Purpose: tasks.RuntimeRecoveryPreparedReleasePurpose, OperationID: terminal.OperationID, AgentCredentialGeneration: b.AgentCredentialGeneration, AuthorizationHash: terminal.AuthorizationHash, TerminalProofHash: recoveryReadHash(terminalWire), TerminalReceiptHash: r.TerminalReceiptHash, ReleaseIntentHash: recoveryReadHash(intent), PreparedAt: r.PreparedAt, ObservedAt: r.PreparedAt.Add(time.Nanosecond), Observation: terminal.Observation}
	wire, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(dir, "recovery-prepared-release-submission.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	if acknowledged {
		ack, _ := json.Marshal(RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, ReleaseIntentHash: p.ReleaseIntentHash, ProofHash: recoveryReadHash(wire), RequestHash: recoveryReadHash(wire)})
		if err := os.WriteFile(filepath.Join(dir, "recovery-prepared-release-acknowledgement.json"), ack, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, wire
}

func TestPreparedSubmissionHistoryTransport(t *testing.T) {
	for _, mode := range []string{"expired_pin", "expired_ack", "rotated", "identity_hash", "identity_error", "absent", "unauthorized", "reservation_cancelled", "redirect", "connection_lost", "wrong_operation", "wrong_intent", "wrong_proof", "wrong_request", "wrong_schema", "not_recorded", "duplicate", "alias", "unknown_field", "trailing", "oversize", "missing_pin", "invalid_ack", "same_bytes_replaced", "ack_created", "archive_changed", "cancelled"} {
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
			var mu sync.Mutex
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				redirects++
				w.WriteHeader(500)
			}))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("unauthenticated request")
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
						http.Error(w, "sensitive response", 401)
						return
					}
					json.NewEncoder(w).Encode(reply)
					return
				}
				reads++
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/prepared-release/verify" || !bytes.Equal(body, pin) {
					t.Error("request was not exact pinned historical verify")
				}
				if tasks.WithHistoricalRecoveryPreparedSubmission(ctx, dir, b, func(tasks.RuntimeRecoveryPreparedSubmissionHistory) error { t.Error("HTTP lost flock"); return nil }) != tasks.ErrRuntimeMutationBlocked {
					t.Error("missing HTTP lock")
				}
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, 307)
					return
				}
				if mode == "connection_lost" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
					} else {
						conn.Close()
					}
					return
				}
				if mode == "absent" || mode == "unauthorized" || mode == "reservation_cancelled" {
					status := 409
					if mode == "unauthorized" {
						status = 401
					}
					http.Error(w, "sensitive response", status)
					return
				}
				var p tasks.RuntimeRecoveryPreparedReleaseProof
				json.Unmarshal(body, &p)
				reply := RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, ReleaseIntentHash: p.ReleaseIntentHash, ProofHash: recoveryReadHash(body), RequestHash: recoveryReadHash(body)}
				write := func(name string, data []byte) {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Error(err)
					}
				}
				switch mode {
				case "wrong_operation":
					reply.OperationID = "foreign"
				case "wrong_intent":
					reply.ReleaseIntentHash = strings.Repeat("0", 64)
				case "wrong_proof":
					reply.ProofHash = strings.Repeat("0", 64)
				case "wrong_request":
					reply.RequestHash = strings.Repeat("0", 64)
				case "wrong_schema":
					reply.SchemaVersion++
				case "not_recorded":
					reply.Recorded = false
				case "same_bytes_replaced":
					write(".replacement", pin)
					if err := os.Rename(filepath.Join(dir, ".replacement"), filepath.Join(dir, "recovery-prepared-release-submission.json")); err != nil {
						t.Error(err)
					}
				case "ack_created":
					data, _ := json.Marshal(reply)
					write("recovery-prepared-release-acknowledgement.json", data)
				case "archive_changed":
					write(filepath.Join("retired-"+g.OperationID, "recovery-final-submission.json"), []byte("{}"))
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
			dir, pin = writePreparedSubmissionHistoryFixture(t, b, g, mode == "expired_ack")
			if mode == "missing_pin" {
				os.Remove(filepath.Join(dir, "recovery-prepared-release-submission.json"))
			}
			if mode == "invalid_ack" {
				if err := os.WriteFile(filepath.Join(dir, "recovery-prepared-release-acknowledgement.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := preparedHistoryTree(t, dir)
			result, err := New(s.URL).ReconcileRecoveryPreparedSubmission(ctx, "isolated-token", b.AgentID, b.ServerID, dir)
			mu.Lock()
			defer mu.Unlock()
			if mode == "expired_pin" || mode == "expired_ack" {
				var p tasks.RuntimeRecoveryPreparedReleaseProof
				json.Unmarshal(pin, &p)
				if err != nil || result != (RecoveryPreparedReleaseReceipt{SchemaVersion: 1, Recorded: true, OperationID: g.OperationID, ReleaseIntentHash: p.ReleaseIntentHash, ProofHash: recoveryReadHash(pin), RequestHash: recoveryReadHash(pin)}) {
					t.Fatal("expired readback unconfirmed/renewed", err)
				}
			} else if err != ErrRecoveryPreparedReleaseUnconfirmed || result != (RecoveryPreparedReleaseReceipt{}) {
				t.Fatal("uncertainty leaked result/details", err)
			}
			if redirects != 0 {
				t.Fatal("redirect followed")
			}
			localInvalid := mode == "rotated" || mode == "identity_hash" || mode == "identity_error" || mode == "missing_pin" || mode == "invalid_ack"
			if (localInvalid && reads != 0) || (!localInvalid && (reads != 1 || calls != 2)) {
				t.Fatal("unexpected read/retry/new evidence", calls, reads)
			}
			switch mode {
			case "ack_created":
				data, _ := os.ReadFile(filepath.Join(dir, "recovery-prepared-release-acknowledgement.json"))
				before["recovery-prepared-release-acknowledgement.json"] = data
			case "archive_changed":
				before[filepath.Join("retired-"+g.OperationID, "recovery-final-submission.json")] = []byte("{}")
			}
			if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
				t.Fatal("historical readback wrote local state")
			}
		})
	}
}
