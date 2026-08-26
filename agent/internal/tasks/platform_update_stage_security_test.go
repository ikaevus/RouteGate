package tasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPlatformUpdateStagerRejectsUnexpectedExistingRootMode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "staging")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivatePlatformUpdateStagingRoot(root); err == nil {
		t.Fatal("staging root with non-private mode was accepted")
	}
}

func TestPlatformUpdateRedirectPolicyAllowsOnlyGitHubHTTPSClosure(t *testing.T) {
	allowed := []string{
		"https://github.com/ikaevus/RouteGate/releases/download/v1.2.3/file",
		"https://release-assets.githubusercontent.com/github-production-release-asset/file",
		"https://objects.githubusercontent.com/file",
	}
	for _, raw := range allowed {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := platformUpdateRedirectPolicy(&http.Request{URL: u}, []*http.Request{{}}); err != nil {
			t.Fatalf("trusted redirect %q rejected: %v", raw, err)
		}
	}

	rejected := []string{
		"http://github.com/file",
		"https://example.com/file",
		"https://user@example.github.com/file",
	}
	for _, raw := range rejected {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := platformUpdateRedirectPolicy(&http.Request{URL: u}, []*http.Request{{}}); err == nil {
			t.Fatalf("untrusted redirect %q was accepted", raw)
		}
	}

	u, _ := url.Parse("https://github.com/file")
	if err := platformUpdateRedirectPolicy(&http.Request{URL: u}, []*http.Request{{}, {}, {}}); err == nil {
		t.Fatal("excessive redirect chain was accepted")
	}
}
