package vpnaccounts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newCanonicalDeviceTestHandler is like newDeviceTestHandler but with a
// configured PublicURL, to exercise deviceSubscriptionURL's canonical path.
func newCanonicalDeviceTestHandler(repo *fakeDeviceRepository, publicURL string) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:                  repo,
		generateSubscriptionToken: GenerateSubscriptionToken,
		publicURL:                 publicURL,
	}
}

// TestDeviceAccessURLUsesConfiguredPublicURLNotRequestHost is the regression
// test for item 4: RouteGate must never issue a device access URL that its
// own canonical Send validator (delivery.extractCanonicalSubscriptionToken)
// would later reject because the Admin UI's request host differs from the
// configured RouteGate PublicURL.
func TestDeviceAccessURLUsesConfiguredPublicURLNotRequestHost(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newCanonicalDeviceTestHandler(repo, "https://vpn.example.com")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"iPhone","clientType":"hiddify","deviceType":"ios"}`))
	request.SetPathValue("id", "account-1")
	// The admin's browser reaches the API through a completely different
	// host than the configured public URL.
	request.Host = "admin.internal.example"
	recorder := httptest.NewRecorder()
	handler.CreateDevice(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response DeviceSubscriptionTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(response.SubscriptionURL, "https://vpn.example.com/sub/") {
		t.Fatalf("subscription URL = %q, want it to use the configured PublicURL origin", response.SubscriptionURL)
	}
}

// TestDeviceAccessURLIgnoresForwardedHeaders proves forwarded headers cannot
// move a device credential's origin: with a valid PublicURL configured,
// X-Forwarded-Host/-Proto must have zero effect on the issued device URL.
func TestDeviceAccessURLIgnoresForwardedHeaders(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newCanonicalDeviceTestHandler(repo, "https://vpn.example.com")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"iPhone","clientType":"hiddify","deviceType":"ios"}`))
	request.SetPathValue("id", "account-1")
	request.Host = "manager.routegate.local"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-Host", "evil.example")
	recorder := httptest.NewRecorder()
	handler.CreateDevice(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response DeviceSubscriptionTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(response.SubscriptionURL, "https://vpn.example.com/sub/") {
		t.Fatalf("subscription URL = %q, forwarded headers must not change the device credential's origin", response.SubscriptionURL)
	}
}

// TestDeviceAccessURLRotateUsesSameCanonicalOrigin proves rotate produces a
// URL on the same canonical origin as create, regardless of request host.
func TestDeviceAccessURLRotateUsesSameCanonicalOrigin(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newCanonicalDeviceTestHandler(repo, "https://vpn.example.com")

	device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Phone", ClientType: "hiddify", DeviceType: "ios"})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices/"+device.ID+"/rotate", nil)
	request.SetPathValue("id", "account-1")
	request.SetPathValue("deviceId", device.ID)
	request.Host = "some-other-host.example"
	recorder := httptest.NewRecorder()
	handler.RotateDeviceSubscriptionToken(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response DeviceSubscriptionTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(response.SubscriptionURL, "https://vpn.example.com/sub/") {
		t.Fatalf("rotated subscription URL = %q, want the same canonical PublicURL origin", response.SubscriptionURL)
	}
}

// TestDeviceAccessURLFallsBackWithoutConfiguredPublicURL documents the
// deliberate parity with the legacy endpoint when PublicURL is not (yet)
// configured: rather than hard-failing device creation, the device URL
// falls back to the same request-derived origin the legacy endpoint always
// uses. Once PublicURL is configured, canonicalization takes over (see the
// tests above) - Send itself already refuses to work without a valid
// PublicURL either way, so this fallback cannot produce a link Send would
// have accepted from a different origin.
func TestDeviceAccessURLFallsBackWithoutConfiguredPublicURL(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo) // no publicURL configured

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"iPhone","clientType":"hiddify","deviceType":"ios"}`))
	request.SetPathValue("id", "account-1")
	request.Host = "manager.routegate.local"
	recorder := httptest.NewRecorder()
	handler.CreateDevice(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response DeviceSubscriptionTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "http://manager.routegate.local/sub/"
	if !strings.HasPrefix(response.SubscriptionURL, want) {
		t.Fatalf("subscription URL = %q, want prefix %q", response.SubscriptionURL, want)
	}
}
