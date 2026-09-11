package vpnaccounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// deviceProtocolTestRepository extends fakeDeviceRepository with the extra
// methods h.clientConnection needs (GetSubscriptionProfileByAccountID,
// GetOrCreateClientProfile) so device compatibility can be exercised against
// a real effective protocol, exactly like public subscription delivery does.
type deviceProtocolTestRepository struct {
	*fakeDeviceRepository
	subscription SubscriptionProfile
	profile      ClientProfile
}

func (f *deviceProtocolTestRepository) GetSubscriptionProfileByAccountID(context.Context, string) (SubscriptionProfile, error) {
	return f.subscription, nil
}

func (f *deviceProtocolTestRepository) GetOrCreateClientProfile(context.Context, string) (ClientProfile, error) {
	return f.profile, nil
}

func (f *deviceProtocolTestRepository) UpdateClientProfile(context.Context, string, UpdateClientProfileRequest) (ClientProfile, error) {
	return ClientProfile{}, nil
}

func vlessDeviceTestSubscription() SubscriptionProfile {
	return SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, ServerID: "server-1", VLESSUUID: testVLESSUUID},
		Server: &SubscriptionServer{
			ID:                "server-1",
			PublicIP:          "203.0.113.10",
			VPNProtocol:       ClientProtocolVLESS,
			VLESSPort:         443,
			VLESSFlow:         "xtls-rprx-vision",
			VLESSNetwork:      "tcp",
			RealityPublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
			RealityShortID:    "0123456789abcdef",
			RealityServerName: "www.example.com",
		},
	}
}

func wireGuardDeviceTestSubscription() SubscriptionProfile {
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	return SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, ServerID: "server-1"},
		Server: &SubscriptionServer{
			ID: "server-1", PublicIP: "203.0.113.10", VPNProtocol: ClientProtocolWireGuard,
			WireGuardPort: 51820, WireGuardDNS: "1.1.1.1", WireGuardPublicKey: key,
		},
		Credentials: SubscriptionCredentials{WireGuard: WireGuardCredentials{PrivateKey: key, PublicKey: key, Address: "10.66.0.2"}},
	}
}

// TestDeviceCompatibilityIsProtocolAware pins Access & Devices to the same
// compatibility truth as /sub/<token> (subscription_delivery.go): a device's
// tier must reflect the account's actual effective protocol, not just its
// selected client type.
func TestDeviceCompatibilityIsProtocolAware(t *testing.T) {
	tests := []struct {
		name         string
		clientType   string
		subscription SubscriptionProfile
		profile      ClientProfile
		wantStatus   string
	}{
		{
			name: "Hiddify on VLESS account is full smart routing", clientType: ClientTypeHiddify,
			subscription: vlessDeviceTestSubscription(),
			profile:      ClientProfile{Protocol: ClientProtocolVLESS, FingerprintMode: FingerprintModeAuto, Fingerprint: DefaultAutoFingerprint, SpiderX: "/"},
			wantStatus:   ClientCompatibilityFullSmartRouting,
		},
		{
			name: "Hiddify on WireGuard account downgrades to connection only", clientType: ClientTypeHiddify,
			subscription: wireGuardDeviceTestSubscription(),
			profile:      ClientProfile{Protocol: ClientProtocolWireGuard},
			wantStatus:   ClientCompatibilityConnectionOnly,
		},
		{
			name: "v2rayN on a share-link-capable protocol requires client setup", clientType: ClientTypeV2RayN,
			subscription: vlessDeviceTestSubscription(),
			profile:      ClientProfile{Protocol: ClientProtocolVLESS, FingerprintMode: FingerprintModeAuto, Fingerprint: DefaultAutoFingerprint, SpiderX: "/"},
			wantStatus:   ClientCompatibilitySetupRequired,
		},
		{
			name: "v2rayNG stays connection only even on the validated VLESS path", clientType: ClientTypeV2RayNG,
			subscription: vlessDeviceTestSubscription(),
			profile:      ClientProfile{Protocol: ClientProtocolVLESS, FingerprintMode: FingerprintModeAuto, Fingerprint: DefaultAutoFingerprint, SpiderX: "/"},
			wantStatus:   ClientCompatibilityConnectionOnly,
		},
		{
			name: "Generic stays connection only on the validated VLESS path", clientType: ClientTypeGeneric,
			subscription: vlessDeviceTestSubscription(),
			profile:      ClientProfile{Protocol: ClientProtocolVLESS, FingerprintMode: FingerprintModeAuto, Fingerprint: DefaultAutoFingerprint, SpiderX: "/"},
			wantStatus:   ClientCompatibilityConnectionOnly,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &deviceProtocolTestRepository{fakeDeviceRepository: newFakeDeviceRepository(), subscription: test.subscription, profile: test.profile}
			device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Device", ClientType: test.clientType, DeviceType: "other"})
			if err != nil {
				t.Fatalf("create device: %v", err)
			}
			handler := newDeviceTestHandler(repo.fakeDeviceRepository)
			handler.accounts = repo

			request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/devices", nil)
			request.SetPathValue("id", "account-1")
			got := handler.deviceCompatibilityAssessment(request.Context(), device)
			if got.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q (assessment: %+v)", got.Status, test.wantStatus, got)
			}
		})
	}
}

// TestDeviceCompatibilityIsConservativeWithoutResolvedProtocol guards the
// "be conservative rather than overclaiming" requirement: when the account's
// effective protocol cannot be resolved at all (no server assignment, no
// client-profile storage), a device must never be reported at a tier above
// connection_only merely because of its selected client type.
func TestDeviceCompatibilityIsConservativeWithoutResolvedProtocol(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)
	device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Device", ClientType: ClientTypeHiddify, DeviceType: "other"})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}

	got := handler.deviceCompatibilityAssessment(context.Background(), device)
	if got.Status != ClientCompatibilityConnectionOnly {
		t.Fatalf("status = %q, want connection_only when no effective protocol can be resolved", got.Status)
	}
	found := false
	for _, code := range got.LimitationCodes {
		if code == LimitationNoEffectiveProtocol {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q limitation code, got %+v", LimitationNoEffectiveProtocol, got.LimitationCodes)
	}
}
