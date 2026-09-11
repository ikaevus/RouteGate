package delivery

import (
	"context"
	"testing"
	"time"
)

func TestExtractSubscriptionTokenParsesSubURL(t *testing.T) {
	token, err := extractSubscriptionToken("https://vpn.example.com/sub/rgsub_abc123")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if token != "rgsub_abc123" {
		t.Fatalf("token = %q, want rgsub_abc123", token)
	}
}

func TestExtractSubscriptionTokenRejectsNonSubscriptionURLs(t *testing.T) {
	for _, raw := range []string{
		"https://vpn.example.com/",
		"https://vpn.example.com/sub/",
		"not a url at all \x7f",
		"",
	} {
		if _, err := extractSubscriptionToken(raw); err == nil {
			t.Fatalf("extractSubscriptionToken(%q) unexpectedly succeeded", raw)
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
