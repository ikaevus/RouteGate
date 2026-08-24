package updates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const (
	managerUpdateStagingRoot = "/var/lib/routegate-manager/update-staging"
	trustedVerifiedGatePath  = "/usr/local/lib/routegate/update/routegate-update-verified.sh"
	officialReleaseAssetBase = "https://github.com/ikaevus/RouteGate/releases/download"

	maxSmallReleaseAssetBytes       int64 = 1 << 20
	maxAttestationBundleBytes       int64 = 16 << 20
	maxReleaseBundleBytes            int64 = 512 << 20
	maxVerifierDescriptorOutputBytes       = 64 << 10
	maxVerifierDiagnosticOutputBytes       = 32 << 10
)

var (
	verifiedCommitPattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
	verifiedSHA256Pattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	verifiedMigrationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type artifactStager interface {
	StageAndVerify(context.Context, string, DiscoveryResult) (StageResult, error)
	Cleanup(string) error
}

type releaseArtifactStager struct {
	client       *http.Client
	stagingRoot  string
	verifierPath string
}

type verifiedDescriptor struct {
	FormatVersion int              `json:"formatVersion"`
	Product       string           `json:"product"`
	Version       string           `json:"version"`
	Commit        string           `json:"commit"`
	BuildDate     string           `json:"buildDate"`
	Database      verifiedDatabase `json:"database"`
	Artifact      VerifiedArtifact `json:"artifact"`
}

type verifiedDatabase struct {
	ExpectedMigration string `json:"expectedMigration"`
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	maxBytes int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.maxBytes - b.buffer.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return original, nil
}

func (b *boundedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func (b *boundedBuffer) String() string {
	return b.buffer.String()
}

func newReleaseArtifactStager() artifactStager {
	return newReleaseArtifactStagerWithDependencies(nil, managerUpdateStagingRoot, trustedVerifiedGatePath)
}

func newReleaseArtifactStagerWithDependencies(client *http.Client, stagingRoot, verifierPath string) artifactStager {
	if client == nil {
		client = &http.Client{}
	} else {
		copyClient := *client
		client = &copyClient
	}
	client.CheckRedirect = stageRedirectPolicy
	return &releaseArtifactStager{
		client:       client,
		stagingRoot:  stagingRoot,
		verifierPath: verifierPath,
	}
}

func stageRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("too many release asset redirects")
	}
	if req.URL.Scheme != "https" || req.URL.User != nil || !allowedReleaseDownloadHost(req.URL.Hostname()) {
		return errors.New("release asset redirect left the fixed GitHub HTTPS boundary")
	}
	return nil
}

func allowedReleaseDownloadHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func (s *releaseArtifactStager) StageAndVerify(ctx context.Context, stageJobID string, discovery DiscoveryResult) (StageResult, error) {
	assets, bundleName, err := validateDiscoveryForStage(discovery)
	if err != nil {
		return StageResult{}, err
	}
	if !uuidPattern.MatchString(stageJobID) {
		return StageResult{}, errors.New("stage job ID is not a UUID")
	}
	if err := validateTrustedVerifierPath(s.verifierPath); err != nil {
		return StageResult{}, err
	}

	if err := os.MkdirAll(s.stagingRoot, 0o700); err != nil {
		return StageResult{}, fmt.Errorf("create update staging root: %w", err)
	}
	if err := validatePrivateDirectory(s.stagingRoot); err != nil {
		return StageResult{}, err
	}

	partialDir := filepath.Join(s.stagingRoot, stageJobID+".partial")
	finalDir := filepath.Join(s.stagingRoot, stageJobID)
	if err := os.RemoveAll(partialDir); err != nil {
		return StageResult{}, fmt.Errorf("clear partial staging directory: %w", err)
	}
	if _, err := os.Lstat(finalDir); err == nil {
		return StageResult{}, errors.New("final staging directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return StageResult{}, fmt.Errorf("inspect final staging directory: %w", err)
	}
	if err := os.Mkdir(partialDir, 0o700); err != nil {
		return StageResult{}, fmt.Errorf("create partial staging directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(partialDir)
		}
	}()

	for _, name := range requiredReleaseAssets(discovery.CandidateVersion, discovery.RuntimeArch) {
		asset := assets[name]
		if err := s.downloadAsset(ctx, discovery.CandidateVersion, name, asset.Size, releaseAssetMaxBytes(name, bundleName), filepath.Join(partialDir, name)); err != nil {
			return StageResult{}, err
		}
	}

	descriptor, err := s.verifyStagedCandidate(ctx, partialDir, bundleName)
	if err != nil {
		return StageResult{}, err
	}
	if err := validateVerifiedDescriptor(descriptor, discovery, assets[bundleName]); err != nil {
		return StageResult{}, err
	}

	if err := os.Rename(partialDir, finalDir); err != nil {
		return StageResult{}, fmt.Errorf("finalize staged candidate: %w", err)
	}
	cleanup = false

	return StageResult{
		CandidateVersion:  discovery.CandidateVersion,
		VerifiedVersion:   descriptor.Version,
		VerifiedCommit:    descriptor.Commit,
		ExpectedMigration: descriptor.Database.ExpectedMigration,
		RuntimeOS:         discovery.RuntimeOS,
		RuntimeArch:       discovery.RuntimeArch,
		Artifact:          descriptor.Artifact,
		ProvenanceStatus:  ProvenanceVerified,
		Verification:      VerificationRG96C3A,
	}, nil
}

func (s *releaseArtifactStager) Cleanup(stageJobID string) error {
	if !uuidPattern.MatchString(stageJobID) {
		return errors.New("stage job ID is not a UUID")
	}
	if err := os.RemoveAll(filepath.Join(s.stagingRoot, stageJobID+".partial")); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.stagingRoot, stageJobID))
}

func validateDiscoveryForStage(discovery DiscoveryResult) (map[string]DiscoveryAsset, string, error) {
	if discovery.Source != DiscoverySourceOfficialGitHub {
		return nil, "", errors.New("discovery source is not the official RouteGate release source")
	}
	if discovery.Availability != AvailabilityUpdateAvailable {
		return nil, "", errors.New("discovery job does not describe an available update")
	}
	if discovery.ProvenanceStatus != ProvenanceUnverified || discovery.VerificationRequired != ProvenanceVerificationRG96B {
		return nil, "", errors.New("discovery provenance contract is not canonical")
	}
	if !releaseTagPattern.MatchString(discovery.CandidateVersion) {
		return nil, "", errors.New("candidate release tag is not safe")
	}
	if discovery.RuntimeOS != "linux" || (discovery.RuntimeArch != "amd64" && discovery.RuntimeArch != "arm64") {
		return nil, "", errors.New("discovery target platform is unsupported")
	}
	if len(discovery.MissingAssets) != 0 {
		return nil, "", errors.New("discovery result is missing required release assets")
	}

	required := requiredReleaseAssets(discovery.CandidateVersion, discovery.RuntimeArch)
	if len(discovery.Assets) != len(required) {
		return nil, "", errors.New("discovery result does not contain the canonical asset set")
	}
	assets := make(map[string]DiscoveryAsset, len(required))
	for _, asset := range discovery.Assets {
		if _, exists := assets[asset.Name]; exists {
			return nil, "", errors.New("discovery result contains duplicate assets")
		}
		assets[asset.Name] = asset
	}
	bundleName := fmt.Sprintf("routegate-%s-linux-%s.tar.gz", discovery.CandidateVersion, discovery.RuntimeArch)
	for _, name := range required {
		asset, ok := assets[name]
		if !ok || asset.Size <= 0 || asset.Size > releaseAssetMaxBytes(name, bundleName) {
			return nil, "", fmt.Errorf("discovery asset is missing or outside the staging size policy: %s", name)
		}
	}
	return assets, bundleName, nil
}

func releaseAssetMaxBytes(name, bundleName string) int64 {
	switch name {
	case "release-manifest.json", "SHA256SUMS":
		return maxSmallReleaseAssetBytes
	case "release-manifest.attestation.json", "release-bundles.attestation.json":
		return maxAttestationBundleBytes
	case bundleName:
		return maxReleaseBundleBytes
	default:
		return 0
	}
}

func (s *releaseArtifactStager) downloadAsset(ctx context.Context, version, name string, expectedSize, maxBytes int64, destination string) error {
	if maxBytes <= 0 || expectedSize <= 0 || expectedSize > maxBytes {
		return errors.New("release asset size violates staging policy")
	}
	assetURL := officialReleaseAssetBase + "/" + url.PathEscape(version) + "/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return fmt.Errorf("build release asset request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "RouteGate-Manager/update-staging")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("download release asset %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download release asset %s returned HTTP %d", name, resp.StatusCode)
	}
	if resp.ContentLength > maxBytes || (resp.ContentLength >= 0 && resp.ContentLength != expectedSize) {
		return fmt.Errorf("release asset %s Content-Length does not match discovery metadata", name)
	}

	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create staged release asset %s: %w", name, err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write staged release asset %s: %w", name, copyErr)
	}
	if written > maxBytes || written != expectedSize {
		return fmt.Errorf("release asset %s size does not match discovery metadata", name)
	}
	if syncErr != nil {
		return fmt.Errorf("sync staged release asset %s: %w", name, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged release asset %s: %w", name, closeErr)
	}
	return nil
}

func (s *releaseArtifactStager) verifyStagedCandidate(ctx context.Context, dir, bundleName string) (verifiedDescriptor, error) {
	args := []string{
		"verify",
		"--manifest", filepath.Join(dir, "release-manifest.json"),
		"--manifest-attestation", filepath.Join(dir, "release-manifest.attestation.json"),
		"--checksums", filepath.Join(dir, "SHA256SUMS"),
		"--bundle", filepath.Join(dir, bundleName),
		"--bundle-attestation", filepath.Join(dir, "release-bundles.attestation.json"),
	}
	cmd := exec.CommandContext(ctx, s.verifierPath, args...)
	stdout := &boundedBuffer{maxBytes: maxVerifierDescriptorOutputBytes}
	stderr := &boundedBuffer{maxBytes: maxVerifierDiagnosticOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return verifiedDescriptor{}, fmt.Errorf("non-mutating release verification failed: %w", err)
	}
	if stdout.overflow || stderr.overflow {
		return verifiedDescriptor{}, errors.New("non-mutating verifier output exceeded the bounded contract")
	}

	var descriptor verifiedDescriptor
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return verifiedDescriptor{}, fmt.Errorf("decode trusted release descriptor: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return verifiedDescriptor{}, err
	}
	return descriptor, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trusted release descriptor contains trailing JSON")
		}
		return fmt.Errorf("read trusted release descriptor trailing data: %w", err)
	}
	return nil
}

func validateVerifiedDescriptor(descriptor verifiedDescriptor, discovery DiscoveryResult, expectedBundle DiscoveryAsset) error {
	if descriptor.FormatVersion != 1 || descriptor.Product != "RouteGate" {
		return errors.New("trusted release descriptor has an unsupported contract")
	}
	if descriptor.Version != discovery.CandidateVersion || !verifiedCommitPattern.MatchString(descriptor.Commit) {
		return errors.New("trusted release descriptor does not match the discovered candidate")
	}
	if !verifiedMigrationPattern.MatchString(descriptor.Database.ExpectedMigration) {
		return errors.New("trusted release descriptor has an invalid migration identifier")
	}
	artifact := descriptor.Artifact
	if artifact.Name != expectedBundle.Name || artifact.OS != discovery.RuntimeOS || artifact.Arch != discovery.RuntimeArch {
		return errors.New("trusted release descriptor target does not match the discovered candidate")
	}
	if !verifiedSHA256Pattern.MatchString(artifact.SHA256) || artifact.Size != expectedBundle.Size || artifact.Size <= 0 {
		return errors.New("trusted release descriptor artifact does not match the staged bundle")
	}
	return nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect staging directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("update staging directory is not a private directory")
	}
	return nil
}

func validateTrustedVerifierPath(path string) error {
	clean := filepath.Clean(path)
	parents := []string{filepath.Dir(filepath.Dir(clean)), filepath.Dir(clean), clean}
	for i, candidate := range parents {
		info, err := os.Lstat(candidate)
		if err != nil {
			return fmt.Errorf("inspect trusted verifier path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("trusted verifier path is symlinked or group/world writable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return errors.New("trusted verifier path is not root-owned")
		}
		if i < len(parents)-1 && !info.IsDir() {
			return errors.New("trusted verifier parent is not a directory")
		}
		if i == len(parents)-1 && (!info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0) {
			return errors.New("trusted verifier is not an executable regular file")
		}
	}
	return nil
}
