package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/revocations"
)

// This adapter is intentionally confined to the isolated HTTP/PostgreSQL test:
// the real Manager router uses the corresponding recoveryTransport adapter.
// Its purpose is to exercise the real Agent HTTP handlers and the real SQL
// service together; it is NOT a mock of the database acceptance decision.
type admissionReleaseHTTPDBTransport struct{ service *revocations.Service }

func (t admissionReleaseHTTPDBTransport) NextRecoveryChallenge(context.Context, string) (json.RawMessage, error) {
	return nil, agents.ErrObservationRejected
}

func (t admissionReleaseHTTPDBTransport) AcceptRecoveryEvidence(context.Context, string, string, []byte) (json.RawMessage, error) {
	return nil, agents.ErrObservationRejected
}

func (t admissionReleaseHTTPDBTransport) AcceptRecoveryAdmissionRelease(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	r, err := t.service.AcceptRecoveryAdmissionRelease(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func (t admissionReleaseHTTPDBTransport) VerifyRecoveryAdmissionRelease(ctx context.Context, token, operation string, data []byte) (json.RawMessage, error) {
	r, err := t.service.VerifyRecoveryAdmissionRelease(ctx, token, operation, data)
	if errors.Is(err, revocations.ErrPreflight) {
		return nil, agents.ErrObservationRejected
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

// The Agent-facing HTTP entrypoint must only acknowledge a proof actually
// committed by the Manager service in isolated PostgreSQL. This does not
// exercise a live Agent owner, sing-box traffic, or reservation completion.
func TestRecoveryAdmissionReleaseHTTPPostgreSQL(t *testing.T) {
	const bearer = "rg_agent_isolated_manager_release_http_bearer"
	// Seed the bearer before recovery reserves the node. Do not bypass its
	// immutability trigger by rewriting an Agent after preparation.
	t.Setenv("ROUTEGATE_TEST_RECOVERY_FIXTURE_AGENT_BEARER", bearer)
	f := newRecoveryAdmissionReleaseFixture(t)

	h := agents.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), f.pool)
	h.SetRecoveryTransport(admissionReleaseHTTPDBTransport{service: f.svc})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agent/recovery-observations/{operation_id}/admission-release", h.RecoveryAdmissionRelease)
	mux.HandleFunc("POST /api/v1/agent/recovery-observations/{operation_id}/admission-release/verify", h.VerifyRecoveryAdmissionRelease)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wire, err := json.Marshal(f.release)
	if err != nil {
		t.Fatal(err)
	}
	operation := f.release.OperationID
	path := "/api/v1/agent/recovery-observations/" + operation + "/admission-release"
	call := func(route, suppliedBearer string, body []byte) (int, []byte) {
		t.Helper()
		req, e := http.NewRequestWithContext(f.ctx, http.MethodPost, srv.URL+route, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		if suppliedBearer != "" {
			req.Header.Set("Authorization", "Bearer "+suppliedBearer)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		response, e := io.ReadAll(io.LimitReader(resp.Body, 8193))
		if e != nil || resp.Header.Get("Cache-Control") != "no-store" || len(response) > 8192 {
			t.Fatal("unbounded or cacheable Manager HTTP response", e)
		}
		return resp.StatusCode, response
	}
	checkCount := func(expected int) {
		t.Helper()
		var count int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_admission_release_evidence`).Scan(&count); e != nil || count != expected {
			t.Fatal("unexpected Manager evidence rows", count, e)
		}
	}
	for _, trial := range []struct {
		name, route, bearer string
		body                []byte
		want                int
	}{
		{"no_bearer", path, "", wire, http.StatusUnauthorized},
		{"foreign_bearer", path, "rg_agent_wrong_isolated_bearer", wire, http.StatusConflict},
		{"wrong_operation", strings.Replace(path, operation, f.actor.ID, 1), bearer, wire, http.StatusConflict},
		{"invalid_json", path, bearer, []byte("{"), http.StatusBadRequest},
		{"oversized", path, bearer, append(append([]byte{}, wire...), bytes.Repeat([]byte(" "), 8192)...), http.StatusBadRequest},
		{"early_verify", path + "/verify", bearer, wire, http.StatusConflict},
	} {
		t.Run(trial.name, func(t *testing.T) {
			status, _ := call(trial.route, trial.bearer, trial.body)
			if status != trial.want {
				t.Fatal("unsafe Agent HTTP acceptance", status, trial.want)
			}
			checkCount(0)
		})
	}

	status, body := call(path, bearer, wire)
	if status != http.StatusOK {
		t.Fatal("Manager declined valid post-release proof", status, string(body))
	}
	var receipt revocations.RecoveryAdmissionReleaseReceipt
	if err = json.Unmarshal(body, &receipt); err != nil || !receipt.Recorded || receipt.SchemaVersion != 1 ||
		receipt.OperationID != operation || receipt.ProofHash != recoveryTestHash(wire) || receipt.RequestHash != receipt.ProofHash {
		t.Fatal("HTTP receipt did not bind immutable statement", err)
	}
	checkCount(1)
	status, body = call(path+"/verify", bearer, wire)
	if status != http.StatusOK {
		t.Fatal("historical Manager HTTP verification refused", status, string(body))
	}
	var verified revocations.RecoveryAdmissionReleaseReceipt
	if err = json.Unmarshal(body, &verified); err != nil || verified != receipt {
		t.Fatal("historical HTTP receipt drift", err)
	}
	if status, _ = call(path, bearer, wire); status != http.StatusOK {
		t.Fatal("identical HTTP retry rejected", status)
	}
	changed := f.release
	changed.CleanedArchiveHash = strings.Repeat("f", 64)
	bad, _ := json.Marshal(changed)
	if status, _ = call(path, bearer, bad); status != http.StatusConflict {
		t.Fatal("different proof overwrote accepted evidence", status)
	}
	checkCount(1)
	var saved, state string
	if err = f.pool.QueryRow(f.ctx, `SELECT e.proof,p.state FROM runtime_recovery_admission_release_evidence e
		JOIN runtime_recovery_preparations p ON p.id=e.preparation_id`).Scan(&saved, &state); err != nil || saved != string(wire) || state != "prepared" {
		t.Fatal("HTTP changed reservation or immutable proof", err)
	}

	// Loss of human release authority blocks future POST acceptance, but does
	// not rewrite the earlier evidence or fabricate a fresh runtime observation.
	f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
	if status, _ = call(path, bearer, wire); status != http.StatusConflict {
		t.Fatal("revoked operator authorized an HTTP write", status)
	}
	if status, body = call(path+"/verify", bearer, wire); status != http.StatusOK {
		t.Fatal("immutable historical readback lost after authority revocation", status, string(body))
	}
	checkCount(1)
}
