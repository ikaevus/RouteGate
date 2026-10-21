package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func admissionReleaseWireFixture(t *testing.T) (tasks.RuntimeRecoveryAdmissionReleaseProof, []byte) {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/recovery-admission-release-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Wire   string `json:"wire"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var p tasks.RuntimeRecoveryAdmissionReleaseProof
	if err := json.Unmarshal([]byte(fixture.Wire), &p); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(p)
	if err != nil || string(wire) != fixture.Wire || recoveryReadHash(wire) != fixture.SHA256 {
		t.Fatal("Agent/Manager release wire fixture drift", err)
	}
	return p, wire
}

func TestRecoveryAdmissionReleaseReadbackTransport(t *testing.T) {
	p, wire := admissionReleaseWireFixture(t)
	for _, mode := range []string{"valid", "wrong_schema", "not_recorded", "wrong_operation", "wrong_proof_hash", "wrong_request_hash", "unknown_field", "malformed", "rejected", "redirect", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				got, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/agent/recovery-observations/"+p.OperationID+"/admission-release/verify" ||
					r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" || !bytes.Equal(got, wire) {
					t.Error("readback escaped original wire, operation or bearer binding")
				}
				switch mode {
				case "rejected":
					w.WriteHeader(http.StatusConflict)
					return
				case "redirect":
					w.Header().Set("Location", "/somewhere-else")
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				case "malformed":
					w.Write([]byte(`{"recorded":true,"broken":`))
					return
				}
				rp := RecoveryAdmissionReleaseReceipt{1, true, p.OperationID, recoveryReadHash(wire), recoveryReadHash(wire)}
				switch mode {
				case "wrong_schema":
					rp.SchemaVersion++
				case "not_recorded":
					rp.Recorded = false
				case "wrong_operation":
					rp.OperationID = p.Observation.AgentID
				case "wrong_proof_hash":
					rp.ProofHash = strings.Repeat("f", 64)
				case "wrong_request_hash":
					rp.RequestHash = strings.Repeat("f", 64)
				}
				if mode == "unknown_field" {
					w.Write([]byte(`{"schemaVersion":1,"recorded":true,"operationId":"` + p.OperationID + `","proofHash":"` + recoveryReadHash(wire) + `","requestHash":"` + recoveryReadHash(wire) + `","unknown":true}`))
					return
				}
				json.NewEncoder(w).Encode(rp)
			}))
			defer s.Close()
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			c := New(s.URL)
			r, err := c.readRecoveryAdmissionReleaseReceipt(ctx, "isolated-token", p)
			if mode == "valid" {
				if err != nil || !validAdmissionReleaseReceipt(r, p, recoveryReadHash(wire)) || calls.Load() != 1 {
					t.Fatal("exact proof was not historically confirmed", err)
				}
			} else if err == nil || r != (RecoveryAdmissionReleaseReceipt{}) {
				t.Fatal("unconfirmed proof was treated as Manager accepted", mode, err)
			}
		})
	}
}

func TestRecoveryAdmissionReleaseReadbackNeverReleasesWithoutLocalHistory(t *testing.T) {
	p, _ := admissionReleaseWireFixture(t)
	var identities, verifies atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/identity/verify" {
			verifies.Add(1)
			w.WriteHeader(http.StatusConflict)
			return
		}
		identities.Add(1)
		data, _ := io.ReadAll(r.Body)
		var q credentialIdentityRequest
		json.Unmarshal(data, &q)
		json.NewEncoder(w).Encode(credentialIdentityReply{1, true, q.AgentID, q.ServerID, p.AgentCredentialGeneration, recoveryReadHash(data)})
	}))
	defer s.Close()
	c := New(s.URL)
	r, err := c.ReconcileRecoveryAdmissionRelease(context.Background(), "isolated-token", p.Observation.AgentID, p.Observation.ServerID, t.TempDir(), p.OperationID)
	if err == nil || r != (RecoveryAdmissionReleaseReceipt{}) || identities.Load() != 1 || verifies.Load() != 0 {
		t.Fatal("missing original locked history must fail closed", err)
	}
}
