package agents

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recoveryCommitTransportFake struct {
	recoveryTransportFake
	verify bool
}

func (f *recoveryCommitTransportFake) AcceptRecoveryCommit(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	return f.AcceptRecoveryEvidence(ctx, token, operation, data)
}
func (f *recoveryCommitTransportFake) VerifyRecoveryCommit(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	f.verify = true
	return f.AcceptRecoveryEvidence(ctx, token, operation, data)
}

func TestRuntimeMutationRecoveryCommitHandlers(t *testing.T) {
	for _, verify := range []bool{false, true} {
		for _, mode := range []string{"valid", "no_auth", "invalid", "oversize", "rejected", "database", "unsupported"} {
			t.Run(mode+map[bool]string{true: "_verify", false: "_accept"}[verify], func(t *testing.T) {
				f := &recoveryCommitTransportFake{}
				want := 200
				body := `{"schemaVersion":1}`
				switch mode {
				case "no_auth":
					want = 401
				case "invalid":
					body = "{"
					want = 400
				case "oversize":
					body += strings.Repeat(" ", 8192)
					want = 400
				case "rejected":
					f.err = ErrObservationRejected
					want = 409
				case "database":
					f.err = errors.New("secret-payload-nonce")
					want = 500
				case "unsupported":
					want = 501
				}
				var logs strings.Builder
				h := &Handler{logger: slog.New(slog.NewTextHandler(&logs, nil)), recovery: f}
				if mode == "unsupported" {
					h.recovery = &recoveryTransportFake{}
				}
				r := httptest.NewRequest(http.MethodPost, "/commit", strings.NewReader(body))
				r.SetPathValue("operation_id", "operation")
				if mode != "no_auth" {
					r.Header.Set("Authorization", "Bearer rg_agent_isolated")
				}
				w := httptest.NewRecorder()
				if verify {
					h.VerifyRecoveryCommit(w, r)
				} else {
					h.RecoveryCommit(w, r)
				}
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(logs.String()+w.Body.String(), "secret-payload-nonce") {
					t.Fatal(w.Code, w.Body.String())
				}
				if want == 200 && (f.token != HashToken("rg_agent_isolated") || f.operation != "operation" || string(f.data) != body || f.verify != verify) {
					t.Fatal("binding/mode lost")
				}
				if (want == 400 || want == 401 || want == 501) && f.calls != 0 {
					t.Fatal("unsafe service call")
				}
			})
		}
	}
}
