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

type recoveryReleaseFake struct{ recoveryTransportFake }

func (f *recoveryReleaseFake) ReadRecoveryReleaseAuthorization(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	return f.AcceptRecoveryEvidence(ctx, token, operation, data)
}
func TestRecoveryReleaseHandler(t *testing.T) {
	for _, mode := range []string{"valid", "no_auth", "invalid", "oversize", "unsupported", "rejected", "database"} {
		t.Run(mode, func(t *testing.T) {
			f := &recoveryReleaseFake{}
			var logs strings.Builder
			h := &Handler{logger: slog.New(slog.NewTextHandler(&logs, nil)), recovery: f}
			body := `{"schemaVersion":1}`
			want := 200
			switch mode {
			case "no_auth":
				want = 401
			case "invalid":
				body = "{"
				want = 400
			case "oversize":
				body += strings.Repeat(" ", 4096)
				want = 400
			case "unsupported":
				h.recovery = &recoveryTransportFake{}
				want = 501
			case "rejected":
				f.err = ErrObservationRejected
				want = 409
			case "database":
				f.err = errors.New("secret-payload-nonce")
				want = 500
			}
			r := httptest.NewRequest(http.MethodPost, "/release-authorization", strings.NewReader(body))
			r.SetPathValue("operation_id", "operation")
			if mode != "no_auth" {
				r.Header.Set("Authorization", "Bearer rg_agent_isolated")
			}
			w := httptest.NewRecorder()
			h.RecoveryReleaseAuthorization(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(logs.String()+w.Body.String(), "secret-payload-nonce") {
				t.Fatal(w.Code, w.Body.String())
			}
			if want == 200 && (f.token != HashToken("rg_agent_isolated") || f.operation != "operation" || string(f.data) != body) {
				t.Fatal("binding lost")
			}
			if (want == 400 || want == 401 || want == 501) && f.calls != 0 {
				t.Fatal("unsafe service call")
			}
		})
	}
}
