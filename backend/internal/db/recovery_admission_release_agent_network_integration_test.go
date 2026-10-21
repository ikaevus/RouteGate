package db

import (
	"bytes"
	"context"
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

// A real Agent Go client process verifies a statement retained via actual
// Manager HTTP + isolated PostgreSQL. This is a cross-module *historical*
// readback test, not an original-owner release or VPN traffic E2E test.
func TestRecoveryAdmissionReleaseAgentNetwork(t *testing.T) {
	if os.Getenv("ROUTEGATE_TEST_RG140_AGENT_NETWORK") != "1" {
		t.Skip("requires explicitly isolated cross-module Manager/Agent test")
	}
	const bearer = "rg_agent_isolated_crossmodule_release"
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
	ctx, cancel := context.WithTimeout(context.Background(), 105*time.Second)
	defer cancel()
	path := "/api/v1/agent/recovery-observations/" + f.release.OperationID + "/admission-release"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+path, bytes.NewReader(wire))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("isolated Manager did not durably accept post-release claim", resp.StatusCode)
	}

	// Child process is a separately built Agent Go module; it cannot import
	// Manager's internals. Give it only the private loopback endpoint, test
	// bearer and frozen statement, without any runtime-mutation authority.
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/client",
		"-run", "^TestRecoveryAdmissionReleaseAgainstManagerPostgreSQL$",
		"-count=1", "-timeout=85s", "-v")
	cmd.Dir = "../../../agent"
	cmd.Env = append(os.Environ(),
		"ROUTEGATE_RG140_CLIENT_URL="+srv.URL,
		"ROUTEGATE_RG140_CLIENT_TOKEN="+bearer,
		"ROUTEGATE_RG140_CLIENT_WIRE_B64="+base64.RawStdEncoding.EncodeToString(wire))
	if output, e := cmd.CombinedOutput(); e != nil {
		// The isolated Agent test never prints the bearer or proof bytes.
		t.Fatalf("cross-module Agent/Manager historical readback failed: %v: %s", e, output)
	}
	var stored, state string
	if err = f.pool.QueryRow(ctx, `SELECT e.proof,p.state FROM runtime_recovery_admission_release_evidence e
		JOIN runtime_recovery_preparations p ON p.id=e.preparation_id`).Scan(&stored, &state); err != nil || stored != string(wire) || state != "prepared" {
		t.Fatal("cross-module network verification mutated the Manager reservation or evidence", err)
	}
}
