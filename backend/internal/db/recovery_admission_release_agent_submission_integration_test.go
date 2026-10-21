package db

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

// Isolated real-Agent POST -> Manager HTTP -> PostgreSQL ledger, followed by
// historical readback. This exercises typed network and transaction admission,
// not original live owner release nor sing-box runtime credential removal.
func TestRecoveryAdmissionReleaseAgentSubmissionNetwork(t *testing.T) {
	if os.Getenv("ROUTEGATE_TEST_RG140_AGENT_SUBMIT") != "1" {
		t.Skip("cross-module submission requires explicit isolated CI switch")
	}
	const bearer = "rg_agent_isolated_crossmodule_submit"
	t.Setenv("ROUTEGATE_TEST_RECOVERY_FIXTURE_AGENT_BEARER", bearer)
	f := newRecoveryAdmissionReleaseFixture(t)
	h := agents.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), f.pool)
	h.SetRecoveryTransport(admissionReleaseHTTPDBTransport{service: f.svc})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agent/recovery-observations/{operation_id}/admission-release", h.RecoveryAdmissionRelease)
	mux.HandleFunc("POST /api/v1/agent/recovery-observations/{operation_id}/admission-release/verify", h.VerifyRecoveryAdmissionRelease)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	proof, err := json.Marshal(f.release)
	if err != nil { t.Fatal(err) }
	authority, err := json.Marshal(f.authorization.Authorization)
	if err != nil { t.Fatal(err) }
	ctx, cancel := context.WithTimeout(context.Background(), 105*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/client",
		"-run", "^TestRecoveryAdmissionReleaseSubmitAgainstManagerPostgreSQL$",
		"-count=1", "-timeout=85s", "-v")
	cmd.Dir = "../../../agent"
	cmd.Env = append(os.Environ(),
		"ROUTEGATE_RG140_SUBMIT_URL="+srv.URL,
		"ROUTEGATE_RG140_SUBMIT_TOKEN="+bearer,
		"ROUTEGATE_RG140_SUBMIT_PROOF_B64="+base64.RawStdEncoding.EncodeToString(proof),
		"ROUTEGATE_RG140_SUBMIT_AUTHORITY_B64="+base64.RawStdEncoding.EncodeToString(authority))
	if output, err := cmd.CombinedOutput(); err != nil {
		// The child test never prints proof, bearer or original human nonce.
		t.Fatalf("isolated Agent-to-Manager submission failed: %v: %s", err, output)
	}
	var saved, state string
	if err := f.pool.QueryRow(ctx, `SELECT e.proof,p.state FROM runtime_recovery_admission_release_evidence e
		JOIN runtime_recovery_preparations p ON p.id=e.preparation_id`).Scan(&saved, &state);
		err != nil || saved != string(proof) || state != "prepared" {
		t.Fatal("Agent POST changed immutable proof or completed Manager reservation", err)
	}
}
