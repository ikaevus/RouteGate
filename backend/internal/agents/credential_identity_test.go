package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type identityRepositoryFake struct {
	fakeAgentAPIRepository
	calls int
	hash  string
	err   error
}

func (f *identityRepositoryFake) ReadCredentialIdentity(_ context.Context, hash string, in CredentialIdentityInput) (CredentialIdentity, error) {
	f.calls++
	f.hash = hash
	return CredentialIdentity{AgentID: in.AgentID, ServerID: in.ServerID, AgentCredentialGeneration: 7}, f.err
}

func TestRuntimeMutationRecoveryIdentityHandler(t *testing.T) {
	const valid = `{"schemaVersion":1,"agentId":"33333333-3333-4333-8333-333333333333","serverId":"44444444-4444-4444-8444-444444444444","nonce":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	for _, mode := range []string{"valid", "no_auth", "duplicate", "alias", "unknown", "trailing", "oversize", "nonce", "null", "wrong_token", "database"} {
		t.Run(mode, func(t *testing.T) {
			f := &identityRepositoryFake{}
			body := valid
			want := 400
			switch mode {
			case "valid":
				want = 200
			case "no_auth":
				want = 401
			case "wrong_token":
				want = 401
				f.err = pgx.ErrNoRows
			case "database":
				want = 500
				f.err = errors.New("secret-token-nonce")
			case "duplicate":
				body = strings.Replace(body, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1)
			case "alias":
				body = strings.Replace(body, "agentId", "AgentID", 1)
			case "unknown":
				body = strings.Replace(body, "agentId", "other", 1)
			case "trailing":
				body += "{}"
			case "oversize":
				body += strings.Repeat(" ", 4096)
			case "nonce":
				body = strings.Replace(body, strings.Repeat("a", 64), "bad", 1)
			case "null":
				body = strings.Replace(body, "1", "null", 1)
			}
			var logs strings.Builder
			h := &Handler{repository: f, logger: slog.New(slog.NewTextHandler(&logs, nil))}
			r := httptest.NewRequest(http.MethodPost, "/identity/verify", strings.NewReader(body))
			if mode != "no_auth" {
				r.Header.Set("Authorization", "Bearer rg_agent_isolated")
			}
			w := httptest.NewRecorder()
			h.VerifyCredentialIdentity(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(logs.String()+w.Body.String(), "secret-token-nonce") {
				t.Fatal(w.Code, w.Body.String())
			}
			if want == 400 || mode == "no_auth" {
				if f.calls != 0 {
					t.Fatal("invalid request reached repository")
				}
			}
			if want == 200 {
				var out CredentialIdentity
				sum := sha256.Sum256([]byte(body))
				if json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Verified || out.AgentCredentialGeneration != 7 || out.RequestHash != hex.EncodeToString(sum[:]) || f.hash != HashToken("rg_agent_isolated") {
					t.Fatal("identity digest/binding")
				}
			}
		})
	}
}

func (f *recoveryTransportFake) ReadRecoveryGrant(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	return f.AcceptRecoveryEvidence(ctx, token, operation, data)
}
func TestRuntimeMutationRecoveryGrantHandler(t *testing.T) {
	for _, mode := range []string{"valid", "no_auth", "invalid", "oversize", "rejected", "database"} {
		t.Run(mode, func(t *testing.T) {
			f := &recoveryTransportFake{}
			want := 200
			body := `{"schemaVersion":1}`
			switch mode {
			case "no_auth":
				want = 401
			case "invalid":
				body = "{"
				want = 400
			case "oversize":
				body += strings.Repeat(" ", 4096)
				want = 400
			case "rejected":
				f.err = ErrObservationRejected
				want = 409
			case "database":
				f.err = errors.New("secret-token-nonce")
				want = 500
			}
			var logs strings.Builder
			h := &Handler{recovery: f, logger: slog.New(slog.NewTextHandler(&logs, nil))}
			r := httptest.NewRequest(http.MethodPost, "/grant", strings.NewReader(body))
			r.SetPathValue("operation_id", "operation")
			if mode != "no_auth" {
				r.Header.Set("Authorization", "Bearer rg_agent_isolated")
			}
			w := httptest.NewRecorder()
			h.RecoveryGrant(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(logs.String()+w.Body.String(), "secret-token-nonce") {
				t.Fatal(w.Code, w.Body.String())
			}
			if want == 200 && (f.token != HashToken("rg_agent_isolated") || f.operation != "operation" || string(f.data) != body) {
				t.Fatal("binding")
			}
			if (want == 400 || want == 401) && f.calls != 0 {
				t.Fatal("unsafe service call")
			}
		})
	}
}
