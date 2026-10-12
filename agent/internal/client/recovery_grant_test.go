package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func recoveryReadTestGrant(t *testing.T) tasks.RuntimeRecoveryMutationGrant {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/recovery-grant-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Wire string `json:"wire"`
	}
	if json.Unmarshal(data, &fixture) != nil {
		t.Fatal("fixture")
	}
	var g tasks.RuntimeRecoveryMutationGrant
	if json.Unmarshal([]byte(fixture.Wire), &g) != nil {
		t.Fatal("grant")
	}
	g.IssuedAt = time.Now().UTC().Add(-time.Second)
	g.ExpiresAt = time.Now().UTC().Add(time.Minute)
	return g
}

func TestRuntimeMutationRecoveryGrantRead(t *testing.T) {
	for _, mode := range []string{"valid", "identity_generation_zero", "identity_hash", "identity_id", "identity_false", "identity_schema", "identity_alias", "identity_duplicate", "identity_trailing", "identity_oversize", "identity_error", "grant_generation", "outer_generation", "grant_hash", "grant_operation", "request_hash", "grant_schema", "grant_expired", "grant_future", "grant_duplicate", "grant_unknown", "grant_null", "grant_alias", "grant_trailing", "grant_oversize", "grant_error", "redirect_identity", "redirect_grant"} {
		t.Run(mode, func(t *testing.T) {
			g := recoveryReadTestGrant(t)
			calls := 0
			redirects := 0
			identityNonce := ""
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; w.WriteHeader(500) }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer isolated-token" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("auth/cache")
				}
				data, _ := io.ReadAll(r.Body)
				var reply []byte
				if r.URL.Path == "/api/v1/agent/identity/verify" {
					var in credentialIdentityRequest
					if json.Unmarshal(data, &in) != nil || in.SchemaVersion != 1 || in.AgentID != g.Expected.AgentID || in.ServerID != g.Expected.ServerID || len(in.Nonce) != 64 {
						t.Error("identity request")
					}
					identityNonce = in.Nonce
					out := credentialIdentityReply{1, true, in.AgentID, in.ServerID, 7, recoveryReadHash(data)}
					switch mode {
					case "redirect_identity":
						http.Redirect(w, r, target.URL, 307)
						return
					case "identity_error":
						http.Error(w, "secret-token-nonce", 500)
						return
					case "identity_generation_zero":
						out.AgentCredentialGeneration = 0
					case "identity_hash":
						out.RequestHash = strings.Repeat("0", 64)
					case "identity_id":
						out.AgentID = g.OperationID
					case "identity_false":
						out.Verified = false
					case "identity_schema":
						out.SchemaVersion = 2
					}
					reply, _ = json.Marshal(out)
					if mode == "identity_alias" {
						reply = []byte(strings.Replace(string(reply), "schemaVersion", "SchemaVersion", 1))
					}
					if mode == "identity_duplicate" {
						reply = append([]byte(`{"schemaVersion":1,`), reply[1:]...)
					}
					if mode == "identity_trailing" {
						reply = append(reply, []byte(`{}`)...)
					}
					if mode == "identity_oversize" {
						reply = append(reply, []byte(strings.Repeat(" ", 16384))...)
					}
				} else {
					if r.URL.Path != "/api/v1/agent/recovery-observations/"+g.OperationID+"/grant" {
						t.Error("path")
					}
					var in recoveryGrantRequest
					if json.Unmarshal(data, &in) != nil || in.AgentCredentialGeneration != 7 || in.Nonce == identityNonce || len(in.Nonce) != 64 || in.OperationID != g.OperationID {
						t.Error("independent generation/nonce")
					}
					if mode == "redirect_grant" {
						http.Redirect(w, r, target.URL, 307)
						return
					}
					if mode == "grant_error" {
						http.Error(w, "secret-token-nonce", 409)
						return
					}
					switch mode {
					case "grant_generation":
						g.AgentCredentialGeneration = 8
					case "grant_operation":
						g.OperationID = g.Expected.AgentID
					case "grant_schema":
						g.SchemaVersion = 2
					case "grant_expired":
						g.ExpiresAt = time.Now().Add(-time.Millisecond)
					case "grant_future":
						g.IssuedAt = time.Now().Add(time.Second)
					}
					wire, _ := json.Marshal(g)
					out := recoveryGrantReply{1, in.OperationID, in.AgentID, in.ServerID, 7, recoveryReadHash(data), recoveryReadHash(wire), g}
					if mode == "outer_generation" {
						out.AgentCredentialGeneration = 8
					}
					if mode == "grant_hash" {
						out.GrantHash = strings.Repeat("0", 64)
					}
					if mode == "request_hash" {
						out.RequestHash = strings.Repeat("0", 64)
					}
					reply, _ = json.Marshal(out)
					if mode == "grant_duplicate" {
						reply = []byte(strings.Replace(string(reply), `"purpose":`, `"schemaVersion":1,"purpose":`, 1))
					}
					if mode == "grant_alias" {
						reply = []byte(strings.Replace(string(reply), "agentCredentialGeneration", "AgentCredentialGeneration", 1))
					}
					if mode == "grant_unknown" {
						reply = append([]byte(`{"unknown":true,`), reply[1:]...)
					}
					if mode == "grant_null" {
						reply = []byte(strings.Replace(string(reply), `"schemaVersion":1`, `"schemaVersion":null`, 1))
					}
					if mode == "grant_trailing" {
						reply = append(reply, []byte(`{}`)...)
					}
					if mode == "grant_oversize" {
						reply = append(reply, []byte(strings.Repeat(" ", 16384))...)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write(reply)
			}))
			defer s.Close()
			c := New(s.URL)
			out, err := c.FetchRecoveryGrant(context.Background(), "isolated-token", g.Expected.AgentID, g.Expected.ServerID, g.OperationID)
			if redirects != 0 {
				t.Fatal("redirect followed")
			}
			if mode == "valid" {
				wire, _ := json.Marshal(g)
				if err != nil || calls != 2 || out.Grant != g || out.Binding.AgentCredentialGeneration != 7 || out.Binding.ManagerURL != s.URL || out.GrantHash != recoveryReadHash(wire) {
					t.Fatal("verified read failed", err)
				}
			} else if err != errRecoveryGrantRead || out != (VerifiedRecoveryGrant{}) || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe read accepted or leaked", err)
			}
		})
	}
}
