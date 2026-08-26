package tasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformUpdateStagerRejectsSymlinkStagingRootBeforeDownload(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0700); err != nil {
		t.Fatal(err)
	}
	symlinkRoot := filepath.Join(base, "staging")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatal(err)
	}

	stager := NewPlatformUpdateStager()
	stager.baseURL = server.URL
	stager.stagingRoot = symlinkRoot
	stager.arch = "amd64"
	stager.client = server.Client()

	_, err := stager.Stage(context.Background(), "550e8400-e29b-41d4-a716-446655440000", PlatformUpdateRequest{
		SchemaVersion: PlatformUpdateSchemaVersion,
		TargetVersion: "v1.2.3",
	})
	if err == nil {
		t.Fatal("Stage() accepted symlink staging root")
	}
	if requests != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests)
	}
}
