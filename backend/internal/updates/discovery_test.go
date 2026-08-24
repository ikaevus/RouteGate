package updates

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func releaseResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func validReleaseJSON(t *testing.T, version, arch string) string {
	t.Helper()
	assets := make([]map[string]any, 0, 5)
	for i, name := range requiredReleaseAssets(version, arch) {
		assets = append(assets, map[string]any{"name": name, "size": int64(100 + i)})
	}
	payload, err := json.Marshal(map[string]any{
		"tag_name":     version,
		"draft":        false,
		"prerelease":   false,
		"published_at": "2026-08-24T00:00:00Z",
		"assets":       assets,
		"ignored":      "not persisted",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestOfficialReleaseDiscoverySelectsRequiredPlatformAssets(t *testing.T) {
	called := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called++
		if req.URL.String() != officialLatestReleaseEndpoint {
			t.Fatalf("request URL = %q", req.URL.String())
		}
		if req.Header.Get("User-Agent") != "RouteGate-Manager" || req.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("unexpected headers: %#v", req.Header)
		}
		return releaseResponse(http.StatusOK, validReleaseJSON(t, "v0.3.0", "amd64")), nil
	})}

	result, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("discover release: %v", err)
	}
	if called != 1 {
		t.Fatalf("network calls = %d, want 1", called)
	}
	if result.Availability != AvailabilityUpdateAvailable || result.CandidateVersion != "v0.3.0" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Assets) != 5 || len(result.MissingAssets) != 0 {
		t.Fatalf("assets = %#v, missing = %#v", result.Assets, result.MissingAssets)
	}
	if result.Assets[4].Name != "routegate-v0.3.0-linux-amd64.tar.gz" {
		t.Fatalf("bundle asset = %q", result.Assets[4].Name)
	}
	if result.ProvenanceStatus != ProvenanceUnverified || result.VerificationRequired != ProvenanceVerificationRG96B {
		t.Fatalf("unexpected trust state: %+v", result)
	}
}

func TestReleaseVersionClassificationIsConservative(t *testing.T) {
	cases := map[string]struct {
		current   string
		candidate string
		want      string
	}{
		"up to date":       {"v0.2.0", "v0.2.0", AvailabilityUpToDate},
		"current newer":    {"v0.3.0", "v0.2.0", AvailabilityCurrentNewer},
		"update available": {"v0.2.0", "v0.2.1", AvailabilityUpdateAvailable},
		"dev current":      {"dev", "v0.2.1", AvailabilityUnknownCurrent},
		"unknown current":  {"unknown", "v0.2.1", AvailabilityUnknownCurrent},
		"invalid current":  {"0.2-rc1", "v0.2.1", AvailabilityUnknownCurrent},
		"invalid release":  {"v0.2.0", "v0.2.999999999999999999999999999999999999999999999999", AvailabilityUncomparableRelease},
		"prerelease tag":   {"v0.2.0", "v0.3.0-rc1", AvailabilityUncomparableRelease},
		"non dotted tag":   {"v0.2.0", "v1", AvailabilityUncomparableRelease},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := compareReleaseVersions(tc.current, tc.candidate); got != tc.want {
				t.Fatalf("classification = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOfficialReleaseDiscoveryMarksIncompleteRelease(t *testing.T) {
	body := validReleaseJSON(t, "v0.3.0", "amd64")
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	assets := payload["assets"].([]any)
	payload["assets"] = assets[:4]
	encoded, _ := json.Marshal(payload)

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return releaseResponse(http.StatusOK, string(encoded)), nil
	})}
	result, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("discover release: %v", err)
	}
	if result.Availability != AvailabilityIncompleteRelease || len(result.MissingAssets) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOfficialReleaseDiscoveryTreats404AsNoRelease(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return releaseResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
	})}
	result, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("discover release: %v", err)
	}
	if result.Availability != AvailabilityNoRelease || result.CandidateVersion != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOfficialReleaseDiscoveryRejectsUnsupportedPlatformWithoutNetwork(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}
	result, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "windows", "amd64")
	if err != nil {
		t.Fatalf("discover release: %v", err)
	}
	if called {
		t.Fatal("unsupported platform performed an outbound request")
	}
	if result.Availability != AvailabilityUnsupportedPlatform {
		t.Fatalf("availability = %q", result.Availability)
	}
}

func TestOfficialReleaseDiscoveryRejectsMalformedAndOversizedResponses(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return releaseResponse(http.StatusOK, `{"tag_name":`), nil
		})}
		if _, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64"); err == nil {
			t.Fatal("expected malformed response error")
		}
	})

	t.Run("oversized", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return releaseResponse(http.StatusOK, strings.Repeat("x", maxReleaseResponseBytes+1)), nil
		})}
		if _, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64"); err == nil {
			t.Fatal("expected oversized response error")
		}
	})
}

func TestOfficialReleaseDiscoveryRejectsRedirects(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		resp := releaseResponse(http.StatusFound, "")
		resp.Header.Set("Location", "https://example.com/redirected")
		return resp, nil
	})}
	if _, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64"); err == nil {
		t.Fatal("expected redirect rejection")
	}
}

func TestOfficialReleaseDiscoveryRejectsDuplicateAndExcessiveAssets(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		body := validReleaseJSON(t, "v0.3.0", "amd64")
		var payload map[string]any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		assets := payload["assets"].([]any)
		payload["assets"] = append(assets, assets[0])
		encoded, _ := json.Marshal(payload)
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return releaseResponse(http.StatusOK, string(encoded)), nil
		})}
		if _, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64"); err == nil {
			t.Fatal("expected duplicate asset rejection")
		}
	})

	t.Run("excessive", func(t *testing.T) {
		assets := make([]map[string]any, 0, maxReleaseAssets+1)
		for i := 0; i < maxReleaseAssets+1; i++ {
			assets = append(assets, map[string]any{"name": "asset-" + strings.Repeat("x", i%10) + string(rune('A'+i)), "size": 1})
		}
		payload, _ := json.Marshal(map[string]any{
			"tag_name":     "v0.3.0",
			"draft":        false,
			"prerelease":   false,
			"published_at": "2026-08-24T00:00:00Z",
			"assets":       assets,
		})
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return releaseResponse(http.StatusOK, string(payload)), nil
		})}
		if _, err := NewOfficialReleaseDiscoverer(client).Discover(context.Background(), "v0.2.0", "linux", "amd64"); err == nil {
			t.Fatal("expected excessive asset rejection")
		}
	})
}
