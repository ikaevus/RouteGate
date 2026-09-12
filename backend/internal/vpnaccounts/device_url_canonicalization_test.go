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

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

// newCanonicalDeviceTestHandler is like newDeviceTestHandler but with a
// configured PublicURL, to exercise deviceCanonicalOrigin's canonical path.
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

// TestDeviceAccessURLRejectsCreateWithoutConfiguredPublicURL is the
// regression test for item 2: unlike the legacy endpoint, RG-116 device
// creation must never fall back to a request-derived origin. A missing
// PublicURL must reject the request outright, with the stable
// public_url_missing error code, and must not create a device row.
func TestDeviceAccessURLRejectsCreateWithoutConfiguredPublicURL(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)
	handler.publicURL = "" // not configured

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"iPhone","clientType":"hiddify","deviceType":"ios"}`))
	request.SetPathValue("id", "account-1")
	request.Host = "manager.routegate.local"
	recorder := httptest.NewRecorder()
	handler.CreateDevice(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	var response httpx.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "public_url_missing" {
		t.Fatalf("error status = %q, want public_url_missing", response.Status)
	}
	if len(repo.devices) != 0 {
		t.Fatalf("device row must not be created when PublicURL is missing, got %d devices", len(repo.devices))
	}
}

// TestDeviceAccessURLRejectsCreateWithInvalidPublicURL proves an invalid
// (non-HTTPS/malformed) PublicURL is rejected the same way, with
// public_url_invalid.
func TestDeviceAccessURLRejectsCreateWithInvalidPublicURL(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newCanonicalDeviceTestHandler(repo, "http://vpn.example.com")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"iPhone","clientType":"hiddify","deviceType":"ios"}`))
	request.SetPathValue("id", "account-1")
	recorder := httptest.NewRecorder()
	handler.CreateDevice(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	var response httpx.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "public_url_invalid" {
		t.Fatalf("error status = %q, want public_url_invalid", response.Status)
	}
	if len(repo.devices) != 0 {
		t.Fatalf("device row must not be created when PublicURL is invalid, got %d devices", len(repo.devices))
	}
}

// TestDeviceAccessURLRejectsRotateWithoutConfiguredPublicURL proves rotate
// enforces the same mandatory-PublicURL invariant as create.
func TestDeviceAccessURLRejectsRotateWithoutConfiguredPublicURL(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)

	device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Phone", ClientType: "hiddify", DeviceType: "ios"})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	handler.publicURL = "" // not configured

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices/"+device.ID+"/rotate", nil)
	request.SetPathValue("id", "account-1")
	request.SetPathValue("deviceId", device.ID)
	recorder := httptest.NewRecorder()
	handler.RotateDeviceSubscriptionToken(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	var response httpx.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "public_url_missing" {
		t.Fatalf("error status = %q, want public_url_missing", response.Status)
	}
}

// TestDeviceAccessURLFailedRotateLeavesExistingTokenUntouched is the
// regression test for the "rotate must fail without invalidating the
// existing active device token" requirement: if a device already has an
// active token and rotate is attempted while PublicURL is missing/invalid,
// the old token must remain active/resolvable - the rejection must happen
// before CreateDeviceSubscriptionToken's revoke-then-insert runs.
func TestDeviceAccessURLFailedRotateLeavesExistingTokenUntouched(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newCanonicalDeviceTestHandler(repo, "https://vpn.example.com")

	device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Phone", ClientType: "hiddify", DeviceType: "ios"})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	existingToken, err := repo.CreateDeviceSubscriptionToken(context.Background(), device.ID, HashSubscriptionToken("existing-raw-token"), nil)
	if err != nil {
		t.Fatalf("issue existing device token: %v", err)
	}

	// Now break PublicURL and attempt to rotate.
	handler.publicURL = "not-a-valid-url"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices/"+device.ID+"/rotate", nil)
	request.SetPathValue("id", "account-1")
	request.SetPathValue("deviceId", device.ID)
	recorder := httptest.NewRecorder()
	handler.RotateDeviceSubscriptionToken(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}

	current, err := repo.GetActiveDeviceSubscriptionToken(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("expected device %q to still have an active token after failed rotate: %v", device.ID, err)
	}
	if current.ID != existingToken.ID {
		t.Fatalf("existing device token must remain unchanged after failed rotate, got %+v, want ID %q", current, existingToken.ID)
	}
}
