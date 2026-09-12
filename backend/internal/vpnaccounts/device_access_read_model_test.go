package vpnaccounts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListDevicesReportsHasActiveTokenAccurately is the item-1/5 regression
// test: the Access & Devices read model must distinguish a device with no
// active token (for example a migrated "Default device") from one that has
// one, using the real HasActiveToken field - never inferred from whether a
// token preview happens to be present (ListDevices never returns a preview
// at all; only the one-time create/rotate response does).
func TestListDevicesReportsHasActiveTokenAccurately(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)

	tokenless, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Default device", ClientType: "generic", DeviceType: "other"})
	if err != nil {
		t.Fatalf("create tokenless device: %v", err)
	}
	tokened, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Phone", ClientType: "hiddify", DeviceType: "ios"})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	if _, err := repo.CreateDeviceSubscriptionToken(context.Background(), tokened.ID, "read-model-hash-1", nil); err != nil {
		t.Fatalf("issue token: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/devices", nil)
	request.SetPathValue("id", "account-1")
	recorder := httptest.NewRecorder()
	handler.ListDevices(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Items []DeviceAccess `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byID := map[string]DeviceAccess{}
	for _, item := range response.Items {
		byID[item.Device.ID] = item
	}

	if got := byID[tokenless.ID]; got.HasActiveToken {
		t.Fatalf("tokenless device hasActiveToken = true, want false: %+v", got)
	}
	if got := byID[tokened.ID]; !got.HasActiveToken {
		t.Fatalf("tokened device hasActiveToken = false, want true: %+v", got)
	}
}

// TestRotateIssuesFirstTokenForTokenlessDeviceWithoutTouchingLegacy is the
// item-1 regression test for reusing the existing rotate/issue operation as
// the "Create access link" action for a device that has no active token yet
// (for example a migrated Default device): after Rotate, the device's own
// HasActiveToken flips to true, and this must never touch the account's
// separate legacy (device_id IS NULL) subscription token.
func TestRotateIssuesFirstTokenForTokenlessDeviceWithoutTouchingLegacy(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)

	device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Default device", ClientType: "generic", DeviceType: "other"})
	if err != nil {
		t.Fatalf("create tokenless device: %v", err)
	}
	legacyToken, err := repo.CreateSubscriptionToken(context.Background(), CreateSubscriptionTokenInput{VPNAccountID: "account-1", TokenHash: "legacy-hash-1"})
	if err != nil {
		t.Fatalf("issue legacy token: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices/"+device.ID+"/rotate", nil)
	request.SetPathValue("id", "account-1")
	request.SetPathValue("deviceId", device.ID)
	recorder := httptest.NewRecorder()
	handler.RotateDeviceSubscriptionToken(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response DeviceSubscriptionTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SubscriptionToken == "" {
		t.Fatalf("expected a freshly issued device token, got empty: %+v", response)
	}

	active, err := repo.GetActiveDeviceSubscriptionToken(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("expected device to now have an active token: %v", err)
	}
	if active.ID == "" {
		t.Fatal("expected a non-empty active device token ID")
	}

	// The legacy token must be completely unaffected by issuing the
	// device's first token.
	stillLegacy, ok := repo.tokensByID[legacyToken.ID]
	if !ok || stillLegacy.Status != SubscriptionTokenStatusActive {
		t.Fatalf("legacy token must remain active and untouched, got %+v (ok=%v)", stillLegacy, ok)
	}
}
