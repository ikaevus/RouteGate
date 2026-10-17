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

func writeRetentionSubmissionHistoryFixture(t *testing.T, b tasks.RuntimeRecoveryGrantBinding, g tasks.RuntimeRecoveryMutationGrant, acknowledged bool) (string, []byte) {
	t.Helper()
	dir, preparedWire := writePreparedSubmissionHistoryFixture(t, b, g, true)
	var prepared tasks.RuntimeRecoveryPreparedReleaseProof
	if err := json.Unmarshal(preparedWire, &prepared); err != nil {
		t.Fatal(err)
	}
	read := func(base, name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	type retainedFile struct {
		Scope string `json:"scope"`
		Name  string `json:"name"`
		Hash  string `json:"hash"`
		Data  []byte `json:"data"`
	}
	retained := struct {
		SchemaVersion     int                               `json:"schemaVersion"`
		Purpose           string                            `json:"purpose"`
		OperationID       string                            `json:"operationId"`
		Binding           tasks.RuntimeRecoveryGrantBinding `json:"binding"`
		AuthorizationHash string                            `json:"authorizationHash"`
		PreparedProofHash string                            `json:"preparedProofHash"`
		RetainedAt        time.Time                         `json:"retainedAt"`
		Files             []retainedFile                    `json:"files"`
	}{SchemaVersion: 1, Purpose: "runtime_mutation_fence_release_retained_history_v1", OperationID: prepared.OperationID, Binding: b, AuthorizationHash: prepared.AuthorizationHash, PreparedProofHash: recoveryReadHash(preparedWire), RetainedAt: prepared.ObservedAt.Add(time.Nanosecond)}
	for _, name := range []string{"recovery-retirement-intent.json", "recovery-retirement-terminal.json", "recovery-terminal-submission.json", "recovery-terminal-acknowledgement.json", "mutation-inflight.json", "recovery-fence-release-intent.json", "recovery-prepared-release-submission.json", "recovery-prepared-release-acknowledgement.json"} {
		data := read(dir, name)
		retained.Files = append(retained.Files, retainedFile{"root", name, recoveryReadHash(data), data})
	}
	archive := filepath.Join(dir, "retired-"+prepared.OperationID)
	entries, err := os.ReadDir(archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data := read(archive, entry.Name())
		retained.Files = append(retained.Files, retainedFile{"archive", entry.Name(), recoveryReadHash(data), data})
	}
	data, _ := json.Marshal(retained)
	if err := os.WriteFile(filepath.Join(dir, "recovery-fence-release-retention.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p := tasks.RuntimeRecoveryRetentionProof{SchemaVersion: 1, Purpose: tasks.RuntimeRecoveryRetentionPurpose, OperationID: prepared.OperationID, AgentCredentialGeneration: b.AgentCredentialGeneration, AuthorizationHash: prepared.AuthorizationHash, PreparedProofHash: recoveryReadHash(preparedWire), PreparedReceiptHash: recoveryReadHash(read(dir, "recovery-prepared-release-acknowledgement.json")), RetentionHash: recoveryReadHash(data), RetainedAt: retained.RetainedAt, ObservedAt: retained.RetainedAt.Add(time.Nanosecond), Observation: prepared.Observation}
	wire, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(dir, "recovery-retention-submission.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	if acknowledged {
		hash := recoveryReadHash(wire)
		ack, _ := json.Marshal(RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, RetentionHash: p.RetentionHash, ProofHash: hash, RequestHash: hash})
		if err := os.WriteFile(filepath.Join(dir, "recovery-retention-acknowledgement.json"), ack, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, wire
}

func TestRetentionSubmissionHistoryTransport(t *testing.T) {
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
				if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/retention/verify" || !bytes.Equal(body, pin) {
					t.Error("request was not exact pinned historical verify")
				}
				if tasks.WithHistoricalRecoveryRetention(ctx, dir, b, func(tasks.RuntimeRecoveryRetentionHistory) error { t.Error("HTTP lost flock"); return nil }) != tasks.ErrRuntimeMutationBlocked {
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
				var p tasks.RuntimeRecoveryRetentionProof
				json.Unmarshal(body, &p)
				reply := RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: p.OperationID, RetentionHash: p.RetentionHash, ProofHash: recoveryReadHash(body), RequestHash: recoveryReadHash(body)}
				write := func(name string, data []byte) {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Error(err)
					}
				}
				switch mode {
				case "wrong_operation":
					reply.OperationID = "foreign"
				case "wrong_intent":
					reply.RetentionHash = strings.Repeat("0", 64)
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
					if err := os.Rename(filepath.Join(dir, ".replacement"), filepath.Join(dir, "recovery-retention-submission.json")); err != nil {
						t.Error(err)
					}
				case "ack_created":
					data, _ := json.Marshal(reply)
					write("recovery-retention-acknowledgement.json", data)
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
			dir, pin = writeRetentionSubmissionHistoryFixture(t, b, g, mode == "expired_ack")
			if mode == "missing_pin" {
				os.Remove(filepath.Join(dir, "recovery-retention-submission.json"))
			}
			if mode == "invalid_ack" {
				if err := os.WriteFile(filepath.Join(dir, "recovery-retention-acknowledgement.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := preparedHistoryTree(t, dir)
			result, err := New(s.URL).ReconcileRecoveryRetention(ctx, "isolated-token", b.AgentID, b.ServerID, dir)
			mu.Lock()
			defer mu.Unlock()
			if mode == "expired_pin" || mode == "expired_ack" {
				var p tasks.RuntimeRecoveryRetentionProof
				json.Unmarshal(pin, &p)
				if err != nil || result != (RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: g.OperationID, RetentionHash: p.RetentionHash, ProofHash: recoveryReadHash(pin), RequestHash: recoveryReadHash(pin)}) {
					t.Fatal("expired readback unconfirmed/renewed", err)
				}
			} else if err != ErrRecoveryRetentionUnconfirmed || result != (RecoveryRetentionReceipt{}) {
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
				data, _ := os.ReadFile(filepath.Join(dir, "recovery-retention-acknowledgement.json"))
				before["recovery-retention-acknowledgement.json"] = data
			case "archive_changed":
				before[filepath.Join("retired-"+g.OperationID, "recovery-final-submission.json")] = []byte("{}")
			}
			if !reflect.DeepEqual(before, preparedHistoryTree(t, dir)) {
				t.Fatal("historical readback wrote local state")
			}
		})
	}
}
