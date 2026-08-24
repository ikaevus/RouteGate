package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	officialLatestReleaseEndpoint = "https://api.github.com/repos/ikaevus/RouteGate/releases/latest"
	discoveryHTTPTimeout          = 5 * time.Second
	maxReleaseResponseBytes      = 128 * 1024
	maxReleaseAssets             = 64
	maxReleaseTagLength          = 64
	maxReleaseAssetNameLength    = 255
)

var (
	errReleaseRedirect        = errors.New("release discovery redirect rejected")
	errReleaseMetadataInvalid = errors.New("release discovery metadata invalid")
	releaseTagPattern         = regexp.MustCompile(`^v[0-9]+(?:\.[0-9]+)+$`)
)

type releaseDiscoverer interface {
	Discover(context.Context, string, string, string) (DiscoveryResult, error)
}

type OfficialReleaseDiscoverer struct {
	client *http.Client
}

func NewOfficialReleaseDiscoverer(client *http.Client) *OfficialReleaseDiscoverer {
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	clone.Timeout = discoveryHTTPTimeout
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errReleaseRedirect
	}
	return &OfficialReleaseDiscoverer{client: &clone}
}

type githubLatestRelease struct {
	TagName     string               `json:"tag_name"`
	Draft       *bool                `json:"draft"`
	Prerelease  *bool                `json:"prerelease"`
	PublishedAt string               `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name string `json:"name"`
	Size *int64 `json:"size"`
}

func (d *OfficialReleaseDiscoverer) Discover(ctx context.Context, currentVersion, runtimeOS, runtimeArch string) (DiscoveryResult, error) {
	result := baseDiscoveryResult(currentVersion, runtimeOS, runtimeArch)
	if !supportedDiscoveryPlatform(runtimeOS, runtimeArch) {
		result.Availability = AvailabilityUnsupportedPlatform
		return result, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, officialLatestReleaseEndpoint, nil)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("build release discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "RouteGate-Manager")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := d.client.Do(req)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("release discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		result.Availability = AvailabilityNoRelease
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		return DiscoveryResult{}, fmt.Errorf("release discovery returned HTTP %d", resp.StatusCode)
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseResponseBytes+1))
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("read release discovery response: %w", err)
	}
	if len(payload) > maxReleaseResponseBytes {
		return DiscoveryResult{}, fmt.Errorf("%w: response exceeds %d bytes", errReleaseMetadataInvalid, maxReleaseResponseBytes)
	}

	var release githubLatestRelease
	if err := json.Unmarshal(payload, &release); err != nil {
		return DiscoveryResult{}, fmt.Errorf("%w: invalid JSON", errReleaseMetadataInvalid)
	}
	if err := validateLatestRelease(release); err != nil {
		return DiscoveryResult{}, err
	}

	publishedAt, _ := time.Parse(time.RFC3339, release.PublishedAt)
	result.CandidateVersion = release.TagName
	result.PublishedAt = publishedAt.UTC().Format(time.RFC3339)

	required := requiredReleaseAssets(release.TagName, runtimeArch)
	assetsByName := make(map[string]githubReleaseAsset, len(release.Assets))
	for _, asset := range release.Assets {
		assetsByName[asset.Name] = asset
	}
	for _, name := range required {
		asset, ok := assetsByName[name]
		if !ok {
			result.MissingAssets = append(result.MissingAssets, name)
			continue
		}
		result.Assets = append(result.Assets, DiscoveryAsset{Name: asset.Name, Size: *asset.Size})
	}
	if len(result.MissingAssets) > 0 {
		result.Availability = AvailabilityIncompleteRelease
		return result, nil
	}

	result.Availability = compareReleaseVersions(currentVersion, release.TagName)
	return result, nil
}

func baseDiscoveryResult(currentVersion, runtimeOS, runtimeArch string) DiscoveryResult {
	return DiscoveryResult{
		Source:               DiscoverySourceOfficialGitHub,
		CurrentVersion:       strings.TrimSpace(currentVersion),
		RuntimeOS:            runtimeOS,
		RuntimeArch:          runtimeArch,
		Assets:               []DiscoveryAsset{},
		MissingAssets:        []string{},
		ProvenanceStatus:     ProvenanceUnverified,
		VerificationRequired: ProvenanceVerificationRG96B,
	}
}

func supportedDiscoveryPlatform(runtimeOS, runtimeArch string) bool {
	return runtimeOS == "linux" && (runtimeArch == "amd64" || runtimeArch == "arm64")
}

func validateLatestRelease(release githubLatestRelease) error {
	if release.Draft == nil || release.Prerelease == nil {
		return fmt.Errorf("%w: missing release flags", errReleaseMetadataInvalid)
	}
	if *release.Draft || *release.Prerelease {
		return fmt.Errorf("%w: draft or prerelease release", errReleaseMetadataInvalid)
	}
	if len(release.TagName) == 0 || len(release.TagName) > maxReleaseTagLength || !releaseTagPattern.MatchString(release.TagName) {
		return fmt.Errorf("%w: invalid release tag", errReleaseMetadataInvalid)
	}
	if _, err := time.Parse(time.RFC3339, release.PublishedAt); err != nil {
		return fmt.Errorf("%w: invalid publication timestamp", errReleaseMetadataInvalid)
	}
	if len(release.Assets) > maxReleaseAssets {
		return fmt.Errorf("%w: too many release assets", errReleaseMetadataInvalid)
	}
	seen := make(map[string]struct{}, len(release.Assets))
	for _, asset := range release.Assets {
		if len(asset.Name) == 0 || len(asset.Name) > maxReleaseAssetNameLength {
			return fmt.Errorf("%w: invalid asset name", errReleaseMetadataInvalid)
		}
		if asset.Size == nil || *asset.Size < 0 {
			return fmt.Errorf("%w: invalid asset size", errReleaseMetadataInvalid)
		}
		if _, exists := seen[asset.Name]; exists {
			return fmt.Errorf("%w: duplicate asset name", errReleaseMetadataInvalid)
		}
		seen[asset.Name] = struct{}{}
	}
	return nil
}

func requiredReleaseAssets(version, arch string) []string {
	return []string{
		"release-manifest.json",
		"release-manifest.attestation.json",
		"SHA256SUMS",
		"release-bundles.attestation.json",
		fmt.Sprintf("routegate-%s-linux-%s.tar.gz", version, arch),
	}
}

func compareReleaseVersions(currentVersion, candidateVersion string) string {
	current, ok := parseDottedReleaseVersion(currentVersion, false)
	if !ok {
		return AvailabilityUnknownCurrent
	}
	candidate, ok := parseDottedReleaseVersion(candidateVersion, true)
	if !ok {
		return AvailabilityUncomparableRelease
	}

	for len(current) < len(candidate) {
		current = append(current, 0)
	}
	for len(candidate) < len(current) {
		candidate = append(candidate, 0)
	}
	for i := range current {
		if candidate[i] > current[i] {
			return AvailabilityUpdateAvailable
		}
		if candidate[i] < current[i] {
			return AvailabilityCurrentNewer
		}
	}
	return AvailabilityUpToDate
}

func parseDottedReleaseVersion(value string, requireV bool) ([]int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "dev") || strings.EqualFold(value, "unknown") {
		return nil, false
	}
	if requireV {
		if !strings.HasPrefix(value, "v") {
			return nil, false
		}
		value = strings.TrimPrefix(value, "v")
	} else {
		value = strings.TrimPrefix(value, "v")
	}
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return nil, false
	}
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		out = append(out, number)
	}
	return out, true
}
