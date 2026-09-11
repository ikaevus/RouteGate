package vpnaccounts

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// deviceScopedSubscriptionRepository extends vpnClientE2ERepository (which
// already wires a full, working VLESS/Reality account so GetClientSubscription
// can render real client material) with a resolvable device and call
// tracking for GetDeviceByID/MarkDeviceUsed, so the device-scoped branch of
// the real /sub/<token> handler (subscription_delivery.go) can be exercised
// end to end instead of only the legacy (device_id empty) path every other
// e2e test in this package covers.
type deviceScopedSubscriptionRepository struct {
	*vpnClientE2ERepository
	device            Device
	markDeviceUsedIDs []string
}

func (r *deviceScopedSubscriptionRepository) GetDeviceByID(_ context.Context, id string) (Device, error) {
	if r.device.ID == "" || r.device.ID != id {
		return Device{}, pgx.ErrNoRows
	}
	return r.device, nil
}

func (r *deviceScopedSubscriptionRepository) MarkDeviceUsed(_ context.Context, id string) error {
	r.markDeviceUsedIDs = append(r.markDeviceUsedIDs, id)
	return nil
}

// GetOrCreateClientProfile/UpdateClientProfile satisfy clientProfileRepository
// so h.clientConnection (used by GetClientSubscription) can resolve an
// effective protocol via resolveEffectiveClientProtocol's fallback instead of
// failing with "client profile storage is unavailable".
func (r *deviceScopedSubscriptionRepository) GetOrCreateClientProfile(_ context.Context, accountID string) (ClientProfile, error) {
	return ClientProfile{
		VPNAccountID:        accountID,
		FingerprintMode:     FingerprintModeAuto,
		Fingerprint:         DefaultAutoFingerprint,
		ResolvedFingerprint: DefaultAutoFingerprint,
		SpiderX:             "/",
		Protocol:            ClientProtocolAuto,
	}, nil
}

func (r *deviceScopedSubscriptionRepository) UpdateClientProfile(context.Context, string, UpdateClientProfileRequest) (ClientProfile, error) {
	return ClientProfile{}, nil
}

func newDeviceScopedSubscriptionFixture(t *testing.T) (*deviceScopedSubscriptionRepository, *Handler) {
	t.Helper()
	base := newVPNClientE2ERepository()
	if _, err := base.CreateAccount(context.Background(), CreateAccountInput{DisplayName: "Device Owner", ServerID: "server-e2e"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := base.SetAccountStatus(context.Background(), base.account.ID, StatusActive); err != nil {
		t.Fatalf("activate account: %v", err)
	}
	repo := &deviceScopedSubscriptionRepository{
		vpnClientE2ERepository: base,
		device:                 Device{ID: "device-1", VPNAccountID: base.account.ID, Name: "iPhone", ClientType: ClientTypeHiddify, DeviceType: DevicePlatformIOS, Status: DeviceStatusActive},
	}
	if _, err := repo.CreateSubscriptionToken(context.Background(), CreateSubscriptionTokenInput{VPNAccountID: base.account.ID, TokenHash: HashSubscriptionToken(vpnClientE2EToken)}); err != nil {
		t.Fatalf("issue token: %v", err)
	}
	repo.token.DeviceID = repo.device.ID // simulate a device-scoped token, as CreateDeviceSubscriptionToken would produce
	handler := &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:                  repo,
		generateSubscriptionToken: func() (string, error) { return vpnClientE2EToken, nil },
	}
	return repo, handler
}

// TestDeviceScopedClientSubscriptionResolvesCorrectAccountAndMarksUsage is
// the item-7 regression test: a device-scoped token must resolve its own
// account, apply the device's own client type, and - only once delivery
// actually succeeds - mark both the token and the device used.
func TestDeviceScopedClientSubscriptionResolvesCorrectAccountAndMarksUsage(t *testing.T) {
	repo, handler := newDeviceScopedSubscriptionFixture(t)

	request := httptest.NewRequest(http.MethodGet, "/sub/"+vpnClientE2EToken, nil)
	request.SetPathValue("token", vpnClientE2EToken)
	response := httptest.NewRecorder()
	handler.GetClientSubscription(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-RouteGate-Client") != ClientTypeHiddify {
		t.Fatalf("X-RouteGate-Client = %q, want the device's own client type", response.Header().Get("X-RouteGate-Client"))
	}
	if repo.markedUsedTokenID != repo.token.ID {
		t.Fatalf("marked token used = %q, want %q", repo.markedUsedTokenID, repo.token.ID)
	}
	if len(repo.markDeviceUsedIDs) != 1 || repo.markDeviceUsedIDs[0] != repo.device.ID {
		t.Fatalf("marked devices used = %+v, want exactly [%q]", repo.markDeviceUsedIDs, repo.device.ID)
	}
}

// TestDeviceScopedClientSubscriptionRejectsRevokedToken proves a revoked
// device token fails exactly like a revoked legacy token, and never marks
// anything used.
func TestDeviceScopedClientSubscriptionRejectsRevokedToken(t *testing.T) {
	repo, handler := newDeviceScopedSubscriptionFixture(t)
	repo.token.Status = SubscriptionTokenStatusRevoked

	request := httptest.NewRequest(http.MethodGet, "/sub/"+vpnClientE2EToken, nil)
	request.SetPathValue("token", vpnClientE2EToken)
	response := httptest.NewRecorder()
	handler.GetClientSubscription(response, request)

	if response.Code == http.StatusOK {
		t.Fatalf("expected a revoked device token to be rejected, got 200: %s", response.Body.String())
	}
	if repo.markedUsedTokenID != "" || len(repo.markDeviceUsedIDs) != 0 {
		t.Fatalf("revoked token must not mark anything used: token=%q devices=%+v", repo.markedUsedTokenID, repo.markDeviceUsedIDs)
	}
}

// TestDeviceScopedClientSubscriptionRejectsExpiredToken proves an expired
// device token fails the same way an expired legacy token does.
func TestDeviceScopedClientSubscriptionRejectsExpiredToken(t *testing.T) {
	repo, handler := newDeviceScopedSubscriptionFixture(t)
	expired := time.Now().Add(-time.Hour)
	repo.token.ExpiresAt = &expired

	request := httptest.NewRequest(http.MethodGet, "/sub/"+vpnClientE2EToken, nil)
	request.SetPathValue("token", vpnClientE2EToken)
	response := httptest.NewRecorder()
	handler.GetClientSubscription(response, request)

	if response.Code == http.StatusOK {
		t.Fatalf("expected an expired device token to be rejected, got 200: %s", response.Body.String())
	}
}

// TestClientSubscriptionUsageIsNeverMarkedOnDeliveryFailure is the direct
// regression test for the ordering bug this pass fixed: MarkDeviceUsed used
// to run before the payload/headers were confirmed renderable, so a request
// that ultimately failed (e.g. an unavailable delivery format) could still
// have bumped the device's last_used_at while never touching the token's.
// Both must now only be marked once delivery has actually succeeded.
func TestClientSubscriptionUsageIsNeverMarkedOnDeliveryFailure(t *testing.T) {
	repo, handler := newDeviceScopedSubscriptionFixture(t)

	request := httptest.NewRequest(http.MethodGet, "/sub/"+vpnClientE2EToken+"?format=v2rayn-routing", nil)
	request.SetPathValue("token", vpnClientE2EToken)
	response := httptest.NewRecorder()
	handler.GetClientSubscription(response, request)

	if response.Code == http.StatusOK {
		t.Fatalf("expected an unavailable delivery format to fail, got 200: %s", response.Body.String())
	}
	if repo.markedUsedTokenID != "" {
		t.Fatalf("token must not be marked used on a failed request, got %q", repo.markedUsedTokenID)
	}
	if len(repo.markDeviceUsedIDs) != 0 {
		t.Fatalf("device must not be marked used on a failed request, got %+v", repo.markDeviceUsedIDs)
	}
}

// TestLegacyClientSubscriptionStillWorksWithoutADevice pins that a legacy
// (device_id empty) token is unaffected by the device-scoped branch: no
// device lookup happens and only the token's own usage is marked. Uses the
// same client-profile-capable fixture as the device-scoped tests, just
// without attaching the token to a device.
func TestLegacyClientSubscriptionStillWorksWithoutADevice(t *testing.T) {
	repo, handler := newDeviceScopedSubscriptionFixture(t)
	repo.token.DeviceID = "" // legacy: no device attached to this token

	request := httptest.NewRequest(http.MethodGet, "/sub/"+vpnClientE2EToken, nil)
	request.SetPathValue("token", vpnClientE2EToken)
	response := httptest.NewRecorder()
	handler.GetClientSubscription(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.markedUsedTokenID != repo.token.ID {
		t.Fatalf("marked token used = %q, want %q", repo.markedUsedTokenID, repo.token.ID)
	}
	if len(repo.markDeviceUsedIDs) != 0 {
		t.Fatalf("legacy token must never mark a device used, got %+v", repo.markDeviceUsedIDs)
	}
}
