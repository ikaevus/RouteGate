package tasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func privatePlatformUpdateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlatformUpdateStagerStagesOnlyFixedOfficialAssetSet(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("asset"))
	}))
	defer server.Close()

	root := privatePlatformUpdateTempDir(t)
	stager := NewPlatformUpdateStager()
	stager.baseURL = server.URL
	stager.stagingRoot = root
	stager.arch = "amd64"
	stager.client = server.Client()
	stager.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	candidate, err := stager.Stage(context.Background(), "550e8400-e29b-41d4-a716-446655440000", PlatformUpdateRequest{
		SchemaVersion: PlatformUpdateSchemaVersion,
		TargetVersion: "v1.2.3",
	})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if candidate.BundleName != "routegate-v1.2.3-linux-amd64.tar.gz" {
		t.Fatalf("BundleName = %q", candidate.BundleName)
	}

	wantPaths := []string{
		"/v1.2.3/SHA256SUMS",
		"/v1.2.3/release-bundles.attestation.json",
		"/v1.2.3/release-manifest.attestation.json",
		"/v1.2.3/release-manifest.json",
		"/v1.2.3/routegate-v1.2.3-linux-amd64.tar.gz",
	}
	sort.Strings(requested)
	if !reflect.DeepEqual(requested, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requested, wantPaths)
	}

	entries, err := os.ReadDir(candidate.Directory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("staged entries = %d, want 5", len(entries))
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("%s is not regular", entry.Name())
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode = %o, want 600", entry.Name(), info.Mode().Perm())
		}
	}
}

func TestPlatformUpdateStagerRejectsUnsafeIdentityAndArchitectureBeforeDownload(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stager := NewPlatformUpdateStager()
	stager.baseURL = server.URL
	stager.stagingRoot = privatePlatformUpdateTempDir(t)
	stager.client = server.Client()

	request := PlatformUpdateRequest{SchemaVersion: PlatformUpdateSchemaVersion, TargetVersion: "v1.2.3"}
	if _, err := stager.Stage(context.Background(), "../escape", request); err == nil {
		t.Fatal("Stage() accepted unsafe task id")
	}
	stager.arch = "386"
	if _, err := stager.Stage(context.Background(), "550e8400-e29b-41d4-a716-446655440000", request); err == nil {
		t.Fatal("Stage() accepted unsupported architecture")
	}
	if requests != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests)
	}
}

func TestPlatformUpdateStagerRejectsRedirectAndCleansPartialState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/other", http.StatusFound)
	}))
	defer server.Close()

	root := privatePlatformUpdateTempDir(t)
	stager := NewPlatformUpdateStager()
	stager.baseURL = server.URL
	stager.stagingRoot = root
	stager.arch = "arm64"
	stager.client = server.Client()
	stager.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

	_, err := stager.Stage(context.Background(), "550e8400-e29b-41d4-a716-446655440000", PlatformUpdateRequest{
		SchemaVersion: PlatformUpdateSchemaVersion,
		TargetVersion: "v1.2.3",
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status") {
		t.Fatalf("Stage() error = %v, want redirect rejection", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("staging root contains %d entries after failure", len(entries))
	}
}

func TestPlatformUpdateStagerRejectsOversizedAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	root := privatePlatformUpdateTempDir(t)
	stager := NewPlatformUpdateStager()
	stager.baseURL = server.URL
	stager.stagingRoot = root
	stager.arch = "amd64"
	stager.client = server.Client()

	_, err := stager.Stage(context.Background(), "550e8400-e29b-41d4-a716-446655440000", PlatformUpdateRequest{
		SchemaVersion: PlatformUpdateSchemaVersion,
		TargetVersion: "v1.2.3",
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("Stage() error = %v, want size-limit rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "550e8400-e29b-41d4-a716-446655440000")); !os.IsNotExist(statErr) {
		t.Fatalf("final staging directory exists after failed download: %v", statErr)
	}
}
