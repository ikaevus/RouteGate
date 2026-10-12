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

type recoveryTransportFake struct {
	calls            int
	token, operation string
	data             []byte
	err              error
}

func (f *recoveryTransportFake) NextRecoveryChallenge(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"schemaVersion":1}`), f.err
}
func (f *recoveryTransportFake) AcceptRecoveryEvidence(_ context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	f.calls++
	f.token = token
	f.operation = operation
	f.data = data
	return json.RawMessage(`{"accepted":true}`), f.err
}

func TestRuntimeMutationRecoveryEvidenceHandler(t *testing.T) {
	for _, mode := range []string{"valid", "no_auth", "invalid", "oversize", "rejected", "database"} {
		t.Run(mode, func(t *testing.T) {
			f := &recoveryTransportFake{}
			if mode == "rejected" {
				f.err = ErrObservationRejected
			}
			if mode == "database" {
				f.err = errors.New("sensitive payload")
			}
			var logs strings.Builder
			h := &Handler{logger: slog.New(slog.NewTextHandler(&logs, nil)), recovery: f}
			body := `{"schemaVersion":1}`
			if mode == "invalid" {
				body = "{"
			}
			if mode == "oversize" {
				body = strings.Repeat(" ", 8192) + body
			}
			r := httptest.NewRequest(http.MethodPost, "/evidence", strings.NewReader(body))
			r.SetPathValue("operation_id", "operation")
			if mode != "no_auth" {
				r.Header.Set("Authorization", "Bearer rg_agent_isolated")
			}
			w := httptest.NewRecorder()
			h.RecoveryEvidence(w, r)
			want := 200
			switch mode {
			case "no_auth":
				want = 401
			case "invalid", "oversize":
				want = 400
			case "rejected":
				want = 409
			case "database":
				want = 500
			}
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
			if strings.Contains(logs.String()+w.Body.String(), "sensitive") {
				t.Fatal("payload leaked")
			}
			if want == 200 && (f.token != HashToken("rg_agent_isolated") || f.operation != "operation" || string(f.data) != body) {
				t.Fatal("binding lost")
			}
			if (want == 400 || want == 401) && f.calls != 0 {
				t.Fatal("unsafe call reached service")
			}
		})
	}
}

func TestRuntimeMutationRecoveryChallengePoll(t *testing.T) {
	f := &recoveryTransportFake{}
	h := &Handler{logger: slog.Default(), repository: &fakeAgentAPIRepository{}, recovery: f}
	r := httptest.NewRequest(http.MethodGet, "/tasks/next", nil)
	r.Header.Set("Authorization", "Bearer rg_agent_isolated")
	w := httptest.NewRecorder()
	h.NextTask(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"task"`) || !strings.Contains(w.Body.String(), `"recoveryChallenge"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
}
