package system

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

type fakeSchemaVersionReader struct {
	version string
	err     error
}

func (f fakeSchemaVersionReader) AppliedSchemaVersion(context.Context) (string, error) {
	return f.version, f.err
}

func TestVersionEndpointReturnsBuildAndSchemaMetadata(t *testing.T) {
	handler := &Handler{
		logger: slog.Default(),
		reader: fakeSchemaVersionReader{version: "000112_agent_runtime_metrics"},
		info:   buildinfo.Current,
	}

	response := httptest.NewRecorder()
	handler.Version(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/version", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}

	var payload VersionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Manager.Version != "dev" || payload.Manager.GitCommit != "unknown" {
		t.Fatalf("unexpected manager metadata: %+v", payload.Manager)
	}
	if payload.Database.ExpectedSchemaVersion != 112 {
		t.Fatalf("unexpected schema metadata: %+v", payload.Database)
	}
	if payload.Database.AppliedSchemaVersion == nil || *payload.Database.AppliedSchemaVersion != "000112_agent_runtime_metrics" {
		t.Fatalf("unexpected applied schema version: %+v", payload.Database.AppliedSchemaVersion)
	}
	if payload.Update.AutomaticUpdatesSupported {
		t.Fatal("automatic updates must not be reported as supported")
	}
}
