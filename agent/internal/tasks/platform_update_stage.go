package tasks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	platformUpdateReleaseBaseURL = "https://github.com/ikaevus/RouteGate/releases/download"
	platformUpdateStagingRoot    = "/var/lib/routegate-agent/update-staging"
	platformUpdateMetadataLimit  = 8 << 20
	platformUpdateBundleLimit    = 512 << 20
	platformUpdateHTTPTimeout    = 2 * time.Minute
)

var canonicalTaskIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// PlatformUpdateStagedCandidate is a host-local, non-authoritative staging result.
// E2b intentionally does not claim provenance verification or permission to
// mutate the host; E2c must pass these exact files through the fixed trusted
// verified updater before any transaction may start.
type PlatformUpdateStagedCandidate struct {
	TaskID        string
	TargetVersion string
	Architecture  string
	Directory     string
	BundleName    string
}

type PlatformUpdateStager struct {
	client      *http.Client
	baseURL     string
	stagingRoot string
	arch        string
}

func NewPlatformUpdateStager() PlatformUpdateStager {
	return PlatformUpdateStager{
		client: &http.Client{
			Timeout: platformUpdateHTTPTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:     platformUpdateReleaseBaseURL,
		stagingRoot: platformUpdateStagingRoot,
		arch:        runtime.GOARCH,
	}
}

func (s PlatformUpdateStager) Stage(ctx context.Context, taskID string, request PlatformUpdateRequest) (PlatformUpdateStagedCandidate, error) {
	if !canonicalTaskIDPattern.MatchString(taskID) {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("platform update task id must be canonical UUIDv4")
	}
	if _, err := DecodePlatformUpdateRequest(mustMarshalPlatformUpdateRequest(request)); err != nil {
		return PlatformUpdateStagedCandidate{}, err
	}
	if s.arch != "amd64" && s.arch != "arm64" {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("unsupported platform update architecture %q", s.arch)
	}
	if s.client == nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("platform update HTTP client is unavailable")
	}
	if s.baseURL == "" || s.stagingRoot == "" {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("platform update stager policy is incomplete")
	}

	bundleName := fmt.Sprintf("routegate-%s-linux-%s.tar.gz", request.TargetVersion, s.arch)
	assets := []struct {
		name  string
		limit int64
	}{
		{"release-manifest.json", platformUpdateMetadataLimit},
		{"release-manifest.attestation.json", platformUpdateMetadataLimit},
		{"SHA256SUMS", platformUpdateMetadataLimit},
		{"release-bundles.attestation.json", platformUpdateMetadataLimit},
		{bundleName, platformUpdateBundleLimit},
	}

	if err := os.MkdirAll(s.stagingRoot, 0700); err != nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("create platform update staging root: %w", err)
	}
	if err := os.Chmod(s.stagingRoot, 0700); err != nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("secure platform update staging root: %w", err)
	}

	finalDir := filepath.Join(s.stagingRoot, taskID)
	if _, err := os.Lstat(finalDir); err == nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("platform update staging directory already exists")
	} else if !os.IsNotExist(err) {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("inspect platform update staging directory: %w", err)
	}

	partialDir, err := os.MkdirTemp(s.stagingRoot, ".partial-"+taskID+"-")
	if err != nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("create private platform update partial directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(partialDir)
		}
	}()
	if err := os.Chmod(partialDir, 0700); err != nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("secure platform update partial directory: %w", err)
	}

	for _, asset := range assets {
		if err := s.downloadAsset(ctx, request.TargetVersion, asset.name, asset.limit, partialDir); err != nil {
			return PlatformUpdateStagedCandidate{}, err
		}
	}

	if err := os.Rename(partialDir, finalDir); err != nil {
		return PlatformUpdateStagedCandidate{}, fmt.Errorf("finalize platform update staging directory: %w", err)
	}
	cleanup = false

	return PlatformUpdateStagedCandidate{
		TaskID:        taskID,
		TargetVersion: request.TargetVersion,
		Architecture:  s.arch,
		Directory:     finalDir,
		BundleName:    bundleName,
	}, nil
}

func (s PlatformUpdateStager) downloadAsset(ctx context.Context, version, assetName string, limit int64, partialDir string) error {
	url := strings.TrimRight(s.baseURL, "/") + "/" + version + "/" + assetName
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build platform update asset request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("download platform update asset %s: %w", assetName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download platform update asset %s: unexpected HTTP status %d", assetName, resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("platform update asset %s exceeds size limit", assetName)
	}

	destination := filepath.Join(partialDir, assetName)
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create staged platform update asset %s: %w", assetName, err)
	}
	reader := io.LimitReader(resp.Body, limit+1)
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write staged platform update asset %s: %w", assetName, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged platform update asset %s: %w", assetName, closeErr)
	}
	if written > limit {
		return fmt.Errorf("platform update asset %s exceeds size limit", assetName)
	}
	return nil
}

func mustMarshalPlatformUpdateRequest(request PlatformUpdateRequest) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":%d,"targetVersion":%q}`, request.SchemaVersion, request.TargetVersion))
}
