package updates

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestProductionArtifactStagerUsesFixedPaths(t *testing.T) {
	stager, ok := newReleaseArtifactStager().(*releaseArtifactStager)
	if !ok {
		t.Fatal("production artifact stager has an unexpected implementation")
	}
	if stager.stagingRoot != managerUpdateStagingRoot {
		t.Fatalf("staging root = %q, want %q", stager.stagingRoot, managerUpdateStagingRoot)
	}
	if stager.verifierPath != trustedVerifiedGatePath {
		t.Fatalf("verifier path = %q, want %q", stager.verifierPath, trustedVerifiedGatePath)
	}
}

func TestStageAndVerifyDownloadsOnlyCanonicalAssetsAndFinalizesAtomically(t *testing.T) {
	version := "v0.2.0"
	arch := "amd64"
	bundleName := fmt.Sprintf("routegate-%s-linux-%s.tar.gz", version, arch)
	contents := stageFixtureContents(bundleName)
	discovery := stageFixtureDiscovery(version, arch, contents)

	var requested []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requested = append(requested, request.URL.String())
		name := filepath.Base(request.URL.Path)
		body, ok := contents[name]
		if !ok {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Request: request}, nil
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       request,
		}, nil
	})}

	root := filepath.Join(t.TempDir(), "staging")
	verifier := writeVerifierFixture(t, bundleName, int64(len(contents[bundleName])))
	validatedPath := ""
	stager := newReleaseArtifactStagerWithDependencies(client, root, verifier, func(path string) error {
		validatedPath = path
		return nil
	})
	jobID := "11111111-1111-4111-8111-111111111111"

	result, err := stager.StageAndVerify(context.Background(), jobID, discovery)
	if err != nil {
		t.Fatalf("stage and verify: %v", err)
	}
	if validatedPath != verifier {
		t.Fatalf("validated verifier path = %q, want %q", validatedPath, verifier)
	}
	if result.VerifiedVersion != version || result.VerifiedCommit != strings.Repeat("a", 40) {
		t.Fatalf("unexpected verified result: %+v", result)
	}
	if result.ExpectedMigration != "000137_update_job_stage" {
		t.Fatalf("expected migration = %q", result.ExpectedMigration)
	}
	if result.ProvenanceStatus != ProvenanceVerified || result.Verification != VerificationRG96C3A {
		t.Fatalf("unexpected verification state: %+v", result)
	}

	finalDir := filepath.Join(root, jobID)
	if info, err := os.Stat(finalDir); err != nil || !info.IsDir() {
		t.Fatalf("final staging directory missing: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(root, jobID+".partial")); !os.IsNotExist(err) {
		t.Fatalf("partial staging directory still exists: %v", err)
	}
	for name, expected := range contents {
		actual, err := os.ReadFile(filepath.Join(finalDir, name))
		if err != nil {
			t.Fatalf("read staged asset %s: %v", name, err)
		}
		if string(actual) != expected {
			t.Fatalf("staged asset %s changed", name)
		}
	}

	expectedURLs := make([]string, 0, len(contents))
	for _, name := range requiredReleaseAssets(version, arch) {
		expectedURLs = append(expectedURLs, officialReleaseAssetBase+"/"+version+"/"+name)
	}
	sort.Strings(requested)
	sort.Strings(expectedURLs)
	if strings.Join(requested, "\n") != strings.Join(expectedURLs, "\n") {
		t.Fatalf("release asset requests = %#v, want %#v", requested, expectedURLs)
	}
}

func TestStageAndVerifySizeMismatchCleansPartialState(t *testing.T) {
	version := "v0.2.0"
	arch := "amd64"
	bundleName := fmt.Sprintf("routegate-%s-linux-%s.tar.gz", version, arch)
	contents := stageFixtureContents(bundleName)
	discovery := stageFixtureDiscovery(version, arch, contents)
	for index := range discovery.Assets {
		if discovery.Assets[index].Name == bundleName {
			discovery.Assets[index].Size++
		}
	}

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := contents[filepath.Base(request.URL.Path)]
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       request,
		}, nil
	})}
	root := filepath.Join(t.TempDir(), "staging")
	verifier := writeVerifierFixture(t, bundleName, int64(len(contents[bundleName])))
	stager := newReleaseArtifactStagerWithDependencies(client, root, verifier, func(string) error { return nil })
	jobID := "22222222-2222-4222-8222-222222222222"

	if _, err := stager.StageAndVerify(context.Background(), jobID, discovery); err == nil {
		t.Fatal("size-mismatched release unexpectedly staged successfully")
	}
	if _, err := os.Stat(filepath.Join(root, jobID+".partial")); !os.IsNotExist(err) {
		t.Fatalf("partial staging state was not cleaned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, jobID)); !os.IsNotExist(err) {
		t.Fatalf("final staging state exists after failure: %v", err)
	}
}

func TestStageRedirectPolicyStaysInsideGitHubHTTPSBoundary(t *testing.T) {
	allowed, _ := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/example", nil)
	if err := stageRedirectPolicy(allowed, []*http.Request{{}}); err != nil {
		t.Fatalf("GitHub release asset redirect rejected: %v", err)
	}

	for _, rawURL := range []string{
		"http://release-assets.githubusercontent.com/example",
		"https://example.com/release",
		"https://githubusercontent.com.evil.example/release",
		"https://user@example.github.com/release",
	} {
		request, _ := http.NewRequest(http.MethodGet, rawURL, nil)
		if err := stageRedirectPolicy(request, []*http.Request{{}}); err == nil {
			t.Fatalf("unsafe redirect accepted: %s", rawURL)
		}
	}

	tooMany, _ := http.NewRequest(http.MethodGet, "https://github.com/example", nil)
	if err := stageRedirectPolicy(tooMany, []*http.Request{{}, {}, {}}); err == nil {
		t.Fatal("redirect chain longer than policy was accepted")
	}
}

func TestValidateDiscoveryForStageRejectsNonCanonicalCandidate(t *testing.T) {
	bundleName := "routegate-v0.2.0-linux-amd64.tar.gz"
	contents := stageFixtureContents(bundleName)
	base := stageFixtureDiscovery("v0.2.0", "amd64", contents)

	cases := []struct {
		name   string
		mutate func(*DiscoveryResult)
	}{
		{name: "wrong source", mutate: func(value *DiscoveryResult) { value.Source = "caller-controlled" }},
		{name: "not available", mutate: func(value *DiscoveryResult) { value.Availability = AvailabilityUpToDate }},
		{name: "already trusted", mutate: func(value *DiscoveryResult) { value.ProvenanceStatus = ProvenanceVerified }},
		{name: "unsafe tag", mutate: func(value *DiscoveryResult) { value.CandidateVersion = "../v0.2.0" }},
		{name: "unsupported platform", mutate: func(value *DiscoveryResult) { value.RuntimeOS = "windows" }},
		{name: "missing asset", mutate: func(value *DiscoveryResult) { value.Assets = value.Assets[:4] }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Assets = append([]DiscoveryAsset(nil), base.Assets...)
			test.mutate(&value)
			if _, _, err := validateDiscoveryForStage(value); err == nil {
				t.Fatal("non-canonical discovery result was accepted for staging")
			}
		})
	}
}

func stageFixtureContents(bundleName string) map[string]string {
	return map[string]string{
		"release-manifest.json":              "manifest\n",
		"release-manifest.attestation.json":  "{}\n",
		"SHA256SUMS":                         "checksums\n",
		"release-bundles.attestation.json":   "{}\n",
		bundleName:                            "bundle-bytes\n",
	}
}

func stageFixtureDiscovery(version, arch string, contents map[string]string) DiscoveryResult {
	assets := make([]DiscoveryAsset, 0, len(contents))
	for _, name := range requiredReleaseAssets(version, arch) {
		assets = append(assets, DiscoveryAsset{Name: name, Size: int64(len(contents[name]))})
	}
	return DiscoveryResult{
		Source:               DiscoverySourceOfficialGitHub,
		CurrentVersion:       "v0.1.0",
		CandidateVersion:     version,
		RuntimeOS:            "linux",
		RuntimeArch:          arch,
		Assets:               assets,
		MissingAssets:        []string{},
		Availability:         AvailabilityUpdateAvailable,
		ProvenanceStatus:     ProvenanceUnverified,
		VerificationRequired: ProvenanceVerificationRG96B,
	}
}

func writeVerifierFixture(t *testing.T, bundleName string, bundleSize int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routegate-update-verified.sh")
	descriptor := fmt.Sprintf(`{"formatVersion":1,"product":"RouteGate","version":"v0.2.0","commit":"%s","buildDate":"2026-08-24T00:00:00Z","database":{"expectedMigration":"000137_update_job_stage"},"artifact":{"name":"%s","os":"linux","arch":"amd64","sha256":"%s","size":%d}}`, strings.Repeat("a", 40), bundleName, strings.Repeat("b", 64), bundleSize)
	script := "#!/usr/bin/env bash\nset -euo pipefail\n[[ ${1:-} == verify ]] || exit 91\nprintf '%s\\n' '" + descriptor + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write verifier fixture: %v", err)
	}
	return path
}
