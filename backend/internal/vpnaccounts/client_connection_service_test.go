package vpnaccounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type clientConnectionTestSource struct {
	subscription SubscriptionProfile
	profile      ClientProfile
}

func (s clientConnectionTestSource) GetSubscriptionProfileByAccountID(context.Context, string) (SubscriptionProfile, error) {
	return s.subscription, nil
}

func (s clientConnectionTestSource) GetOrCreateClientProfile(context.Context, string) (ClientProfile, error) {
	return s.profile, nil
}

func hysteria2TestSubscription() SubscriptionProfile {
	accountID := "11111111-1111-1111-1111-111111111111"
	return SubscriptionProfile{
		Account: Account{
			ID:          accountID,
			DisplayName: "Protocol override",
			ServerID:    "server-1",
			VLESSUUID:   "0038dfb4-5a0f-44d8-b26a-70d4772443b1",
		},
		Server: &SubscriptionServer{
			ID:              "server-1",
			PublicIP:        "203.0.113.10",
			VPNProtocol:     ClientProtocolVLESS,
			Hysteria2Port:   443,
			Hysteria2Domain: "vpn.example.com",
		},
		Credentials: SubscriptionCredentials{
			Hysteria2: Hysteria2Credentials{
				Username: accountID,
				Password: strings.Repeat("a", 48),
			},
		},
	}
}

func TestBuildClientConnectionExplicitProtocolOverridesNodeDefault(t *testing.T) {
	source := clientConnectionTestSource{
		subscription: hysteria2TestSubscription(),
		profile: ClientProfile{
			Protocol:        ClientProtocolHysteria2,
			FingerprintMode: FingerprintModeAuto,
			Fingerprint:     DefaultAutoFingerprint,
			SpiderX:         "/",
		},
	}

	response, err := BuildClientConnection(context.Background(), source, source.subscription.Account.ID)
	if err != nil {
		t.Fatalf("build client connection: %v", err)
	}
	if response.Protocol != ClientProtocolHysteria2 {
		t.Fatalf("expected effective protocol %q, got %q", ClientProtocolHysteria2, response.Protocol)
	}
	if response.Format != "hysteria2-uri" || !strings.HasPrefix(response.Hysteria2URI, "hysteria2://") {
		t.Fatalf("expected rendered Hysteria2 connection, got format=%q uri=%q", response.Format, response.Hysteria2URI)
	}
}

func TestResolveEffectiveClientProtocolAutoInheritsNodeDefault(t *testing.T) {
	server := &SubscriptionServer{VPNProtocol: ClientProtocolMTProto}
	profile := ClientProfile{Protocol: ClientProtocolAuto}
	if got := resolveEffectiveClientProtocol(profile, server); got != ClientProtocolMTProto {
		t.Fatalf("expected auto to inherit %q, got %q", ClientProtocolMTProto, got)
	}
}

func TestResolveEffectiveClientProtocolDefaultsToVLESS(t *testing.T) {
	server := &SubscriptionServer{}
	profile := ClientProfile{Protocol: ClientProtocolAuto}
	if got := resolveEffectiveClientProtocol(profile, server); got != ClientProtocolVLESS {
		t.Fatalf("expected default %q, got %q", ClientProtocolVLESS, got)
	}
}

func TestBuildClientConnectionReportsUnassignedAccount(t *testing.T) {
	source := clientConnectionTestSource{
		subscription: SubscriptionProfile{Account: Account{ID: "account-1"}},
		profile:      ClientProfile{Protocol: ClientProtocolAuto},
	}
	_, err := BuildClientConnection(context.Background(), source, "account-1")
	if !errors.Is(err, ErrVPNAccountUnassigned) {
		t.Fatalf("expected ErrVPNAccountUnassigned, got %v", err)
	}
}

func TestBuildClientConnectionReportsUnavailableSelectedProtocol(t *testing.T) {
	subscription := hysteria2TestSubscription()
	source := clientConnectionTestSource{
		subscription: subscription,
		profile: ClientProfile{
			Protocol:        ClientProtocolWireGuard,
			FingerprintMode: FingerprintModeAuto,
			Fingerprint:     DefaultAutoFingerprint,
			SpiderX:         "/",
		},
	}
	_, err := BuildClientConnection(context.Background(), source, subscription.Account.ID)
	if !errors.Is(err, ErrClientConnectionUnavailable) {
		t.Fatalf("expected ErrClientConnectionUnavailable, got %v", err)
	}
}

func (f *fakeAccountRepository) GetOrCreateClientProfile(_ context.Context, accountID string) (ClientProfile, error) {
	return ClientProfile{
		VPNAccountID:       accountID,
		Name:               "Default",
		ClientType:         "other",
		DeviceType:         "other",
		FingerprintMode:    FingerprintModeAuto,
		Fingerprint:        DefaultAutoFingerprint,
		ResolvedFingerprint: DefaultAutoFingerprint,
		SpiderX:            "/",
		Protocol:           ClientProtocolAuto,
	}, nil
}

func (f *fakeAccountRepository) UpdateClientProfile(context.Context, string, UpdateClientProfileRequest) (ClientProfile, error) {
	panic("UpdateClientProfile must not run when protocol preflight fails")
}

func TestUpdateClientProfilePreflightsBeforePersistence(t *testing.T) {
	repo := &fakeAccountRepository{profile: hysteria2TestSubscription()}
	handler := newTestHandler(repo)
	body := `{"name":"Default","clientType":"other","deviceType":"other","fingerprintMode":"auto","fingerprint":"firefox","serverNameOverride":"","spiderX":"/","mtu":null,"protocol":"wireguard"}`
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/vpn-accounts/account-1/client-profile", strings.NewReader(body))
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.UpdateClientProfile(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected preflight conflict %d, got %d: %s", http.StatusConflict, response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "client_connection_unavailable") {
		t.Fatalf("expected client_connection_unavailable error, got %s", response.Body.String())
	}
}
