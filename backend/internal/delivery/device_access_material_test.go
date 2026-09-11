package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// fakeDeviceAccessLookup is a minimal in-memory deviceAccessLookup so
// validateDeviceAccessRequestWith's device/token checks can be unit-tested
// without a real database (h.accounts is a concrete *vpnaccounts.Repository
// in production; see the deviceAccessLookup doc comment in handler.go).
type fakeDeviceAccessLookup struct {
	device    vpnaccounts.Device
	deviceErr error
	token     vpnaccounts.SubscriptionToken
	tokenErr  error
}

func (f fakeDeviceAccessLookup) GetDevice(context.Context, string, string) (vpnaccounts.Device, error) {
	return f.device, f.deviceErr
}

func (f fakeDeviceAccessLookup) GetActiveDeviceSubscriptionToken(context.Context, string) (vpnaccounts.SubscriptionToken, error) {
	return f.token, f.tokenErr
}

// TestExtractCanonicalSubscriptionTokenAcceptsOwnCanonicalURL confirms the
// happy path: a /sub/<token> URL on RouteGate's own configured public origin
// is accepted and yields the exact token.
func TestExtractCanonicalSubscriptionTokenAcceptsOwnCanonicalURL(t *testing.T) {
	token, err := extractCanonicalSubscriptionToken("https://vpn.example.com", "https://vpn.example.com/sub/rgsub_abc123")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if token != "rgsub_abc123" {
		t.Fatalf("token = %q, want rgsub_abc123", token)
	}
}

// TestExtractCanonicalSubscriptionTokenRejectsForeignHost is the core
// anti-relay guard: a URL that embeds a syntactically valid token but on a
// host other than RouteGate's own configured public URL must never be
// accepted, or this endpoint could be used to relay mail/Telegram delivery
// through an attacker-chosen link.
func TestExtractCanonicalSubscriptionTokenRejectsForeignHost(t *testing.T) {
	if _, err := extractCanonicalSubscriptionToken("https://vpn.example.com", "https://evil.example/sub/rgsub_abc123"); err == nil {
		t.Fatal("expected foreign host to be rejected")
	}
}

func TestExtractCanonicalSubscriptionTokenRejectsWrongPath(t *testing.T) {
	for _, raw := range []string{
		"https://vpn.example.com/",
		"https://vpn.example.com/sub/",
		"https://vpn.example.com/subscribe/rgsub_abc123",
		"https://vpn.example.com/sub/rgsub_abc123/extra",
		"https://vpn.example.com/sub/rgsub_abc123?format=v2rayn-routing",
		"https://vpn.example.com/sub/rgsub_abc123#fragment",
		"https://user:pass@vpn.example.com/sub/rgsub_abc123",
	} {
		if _, err := extractCanonicalSubscriptionToken("https://vpn.example.com", raw); err == nil {
			t.Fatalf("extractCanonicalSubscriptionToken(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestExtractCanonicalSubscriptionTokenRejectsMalformedURL(t *testing.T) {
	for _, raw := range []string{
		"not a url at all \x7f",
		"",
		"   ",
	} {
		if _, err := extractCanonicalSubscriptionToken("https://vpn.example.com", raw); err == nil {
			t.Fatalf("extractCanonicalSubscriptionToken(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestDeviceAccessMaterialStorePutGetRoundTrip(t *testing.T) {
	store := newDeviceAccessMaterialStore()
	store.put("delivery-1", "https://vpn.example.com/sub/rgsub_token", "iPhone")

	entry, ok := store.get("delivery-1")
	if !ok {
		t.Fatal("expected entry to be present")
	}
	if entry.accessURL != "https://vpn.example.com/sub/rgsub_token" || entry.profileName != "iPhone" {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	if _, ok := store.get("delivery-missing"); ok {
		t.Fatal("expected missing delivery id to be absent")
	}
}

func TestDeviceAccessMaterialStoreExpiresEntries(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := newDeviceAccessMaterialStore()
	store.now = func() time.Time { return now }
	store.ttl = time.Minute

	store.put("delivery-1", "https://vpn.example.com/sub/rgsub_token", "iPhone")
	if _, ok := store.get("delivery-1"); !ok {
		t.Fatal("expected entry to be present before expiry")
	}

	now = now.Add(2 * time.Minute)
	if _, ok := store.get("delivery-1"); ok {
		t.Fatal("expected entry to be gone after TTL elapses")
	}
}

func TestDeviceAccessMaterialStoreIgnoresEmptyKeysOrURLs(t *testing.T) {
	store := newDeviceAccessMaterialStore()
	store.put("", "https://vpn.example.com/sub/rgsub_token", "iPhone")
	store.put("delivery-1", "", "iPhone")

	if _, ok := store.get(""); ok {
		t.Fatal("expected empty delivery id to never be stored")
	}
	if _, ok := store.get("delivery-1"); ok {
		t.Fatal("expected empty access URL to never be stored")
	}
}

// TestVPNAccessResolverResolvesDeviceScopedDeliveryFromStashedMaterial
// verifies the core mechanism item 1/2 rely on: a device-scoped delivery
// (Delivery.DeviceID set) resolves using whatever was stashed via
// StashDeviceAccess for that exact delivery ID, entirely in memory, with no
// database round trip through the account-level BuildClientConnection path.
// If nothing was stashed (expired, or the process restarted), resolution
// must fail permanently instead of fabricating a URL.
func TestVPNAccessResolverResolvesDeviceScopedDeliveryFromStashedMaterial(t *testing.T) {
	resolver := NewVPNAccessResolver(nil, "https://vpn.example.com")
	resolver.StashDeviceAccess("delivery-rg116-stashed-fixture", "https://vpn.example.com/sub/rgsub_token", "iPhone")

	material, err := resolver.Resolve(context.Background(), Delivery{
		ID:           "delivery-rg116-stashed-fixture",
		VPNAccountID: "account-1",
		DeviceID:     "device-1",
		TemplateKey:  TemplateVPNAccess,
	})
	if err != nil {
		t.Fatalf("resolve device-scoped delivery: %v", err)
	}
	if material.TemplateData.ConnectURL != "https://vpn.example.com/sub/rgsub_token" {
		t.Fatalf("ConnectURL = %q, want the stashed device access URL verbatim", material.TemplateData.ConnectURL)
	}
	if material.TemplateData.ProfileName != "iPhone" {
		t.Fatalf("ProfileName = %q, want iPhone", material.TemplateData.ProfileName)
	}
}

func TestVPNAccessResolverFailsPermanentlyWhenDeviceMaterialIsUnavailable(t *testing.T) {
	resolver := NewVPNAccessResolver(nil, "https://vpn.example.com")

	_, err := resolver.Resolve(context.Background(), Delivery{
		ID:           "delivery-rg116-never-stashed-fixture",
		VPNAccountID: "account-1",
		DeviceID:     "device-1",
		TemplateKey:  TemplateVPNAccess,
	})
	failure, ok := err.(Failure)
	if !ok {
		t.Fatalf("expected a Failure, got %T: %v", err, err)
	}
	if failure.Class != ErrorClassPermanent {
		t.Fatalf("error class = %q, want permanent (never silently retry into a fabricated URL)", failure.Class)
	}
	if failure.Code != "device_access_link_unavailable" {
		t.Fatalf("error code = %q, want device_access_link_unavailable", failure.Code)
	}
}

func activeDeviceAccessLookupFixture() fakeDeviceAccessLookup {
	return fakeDeviceAccessLookup{
		device: vpnaccounts.Device{ID: "device-1", VPNAccountID: "account-1", Name: "iPhone", Status: vpnaccounts.DeviceStatusActive},
		token:  vpnaccounts.SubscriptionToken{ID: "token-generation-1", DeviceID: "device-1", TokenHash: vpnaccounts.HashSubscriptionToken("rgsub_current")},
	}
}

// TestValidateDeviceAccessRequestAcceptsTheDevicesOwnCurrentLink is the
// happy path: the canonical URL for the device's own current active token
// is accepted and its token returned untouched for downstream stashing.
func TestValidateDeviceAccessRequestAcceptsTheDevicesOwnCurrentLink(t *testing.T) {
	fixture := activeDeviceAccessLookupFixture()
	_, accessURL, tokenID, failure := validateDeviceAccessRequestWith(
		context.Background(), fixture, "https://vpn.example.com",
		"account-1", "device-1", "https://vpn.example.com/sub/rgsub_current",
	)
	if failure != nil {
		t.Fatalf("expected success, got %+v", failure)
	}
	if accessURL != "https://vpn.example.com/sub/rgsub_current" {
		t.Fatalf("accessURL = %q", accessURL)
	}
	if tokenID != fixture.token.ID {
		t.Fatalf("tokenID = %q, want the active token's own ID %q", tokenID, fixture.token.ID)
	}
}

// TestValidateDeviceAccessRequestRejectsWrongToken is the "wrong token"
// case: a structurally valid canonical URL whose token does not hash to the
// device's own current active token must be rejected exactly like a stale
// or foreign link, never treated as good enough because the URL shape is
// otherwise fine.
func TestValidateDeviceAccessRequestRejectsWrongToken(t *testing.T) {
	_, _, _, failure := validateDeviceAccessRequestWith(
		context.Background(), activeDeviceAccessLookupFixture(), "https://vpn.example.com",
		"account-1", "device-1", "https://vpn.example.com/sub/rgsub_someone_elses_token",
	)
	if failure == nil {
		t.Fatal("expected a wrong-token request to be rejected")
	}
	if failure.code != "device_access_url_stale" {
		t.Fatalf("code = %q, want device_access_url_stale", failure.code)
	}
}

// TestValidateDeviceAccessRequestRejectsForeignHostEvenWithTheRightToken is
// the end-to-end version of the anti-relay guard: even the device's own
// correct token, embedded in a URL on a foreign host, must be rejected -
// otherwise this endpoint could be used to relay delivery through an
// attacker-chosen link.
func TestValidateDeviceAccessRequestRejectsForeignHostEvenWithTheRightToken(t *testing.T) {
	_, _, _, failure := validateDeviceAccessRequestWith(
		context.Background(), activeDeviceAccessLookupFixture(), "https://vpn.example.com",
		"account-1", "device-1", "https://evil.example/sub/rgsub_current",
	)
	if failure == nil {
		t.Fatal("expected a foreign-host request to be rejected even with the correct token")
	}
	if failure.code != "device_access_url_stale" {
		t.Fatalf("code = %q, want device_access_url_stale", failure.code)
	}
}

func TestValidateDeviceAccessRequestRejectsRevokedDevice(t *testing.T) {
	lookup := activeDeviceAccessLookupFixture()
	lookup.device.Status = vpnaccounts.DeviceStatusRevoked
	_, _, _, failure := validateDeviceAccessRequestWith(
		context.Background(), lookup, "https://vpn.example.com",
		"account-1", "device-1", "https://vpn.example.com/sub/rgsub_current",
	)
	if failure == nil || failure.code != "device_revoked" {
		t.Fatalf("expected device_revoked, got %+v", failure)
	}
}

// TestCanonicallyIssuedDeviceURLPassesTheSendValidator cross-checks that a
// URL built the same way vpnaccounts.Handler.deviceSubscriptionURL builds
// one - publicurl.Normalize(publicURL) + "/sub/" + token - always passes
// extractCanonicalSubscriptionToken for that same publicURL. Both call
// sites go through the shared internal/publicurl policy, so issuance and
// validation can never quietly drift apart into "RouteGate issues a link
// its own Send validator then rejects".
func TestCanonicallyIssuedDeviceURLPassesTheSendValidator(t *testing.T) {
	const configuredPublicURL = "https://vpn.example.com"
	canonicalOrigin, err := NormalizePublicURL(configuredPublicURL)
	if err != nil {
		t.Fatalf("normalize configured public url: %v", err)
	}
	issuedURL := canonicalOrigin + "/sub/rgsub_device_fixture"

	token, err := extractCanonicalSubscriptionToken(configuredPublicURL, issuedURL)
	if err != nil {
		t.Fatalf("canonically issued device URL rejected by its own Send validator: %v", err)
	}
	if token != "rgsub_device_fixture" {
		t.Fatalf("token = %q, want rgsub_device_fixture", token)
	}
}
