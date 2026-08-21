package vpnaccounts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type topologyAccountRepository struct {
	*fakeAccountRepository
	clientProfile   ClientProfile
	savedProfile    bool
	validatedServer string
	validatedProto  string
	deploymentRole  string
}

func (f *topologyAccountRepository) GetOrCreateClientProfile(context.Context, string) (ClientProfile, error) {
	return f.clientProfile, nil
}

func (f *topologyAccountRepository) UpdateClientProfile(_ context.Context, accountID string, request UpdateClientProfileRequest) (ClientProfile, error) {
	f.savedProfile = true
	return ClientProfile{
		ID:                  "profile-1",
		VPNAccountID:        accountID,
		Name:                request.Name,
		ClientType:          request.ClientType,
		DeviceType:          request.DeviceType,
		FingerprintMode:     request.FingerprintMode,
		Fingerprint:         request.Fingerprint,
		ResolvedFingerprint: resolveClientFingerprint(clientProfileFromRequest(accountID, request)),
		ServerNameOverride:  request.ServerNameOverride,
		SpiderX:             request.SpiderX,
		MTU:                 request.MTU,
		Protocol:            request.Protocol,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}, nil
}

func (f *topologyAccountRepository) ValidateClientProtocolTopology(_ context.Context, serverID, protocol string) error {
	f.validatedServer = serverID
	f.validatedProto = protocol
	return validateClientProtocolDeploymentRole(protocol, f.deploymentRole)
}

func TestValidateClientProtocolDeploymentRole(t *testing.T) {
	if err := validateClientProtocolDeploymentRole(ClientProtocolHysteria2, "vpn"); err != nil {
		t.Fatalf("dedicated VPN node should support Hysteria2: %v", err)
	}
	if err := validateClientProtocolDeploymentRole(ClientProtocolVLESS, "hybrid"); err != nil {
		t.Fatalf("Hybrid node should support VLESS: %v", err)
	}
	if err := validateClientProtocolDeploymentRole(ClientProtocolHysteria2, "hybrid"); err == nil {
		t.Fatal("Hybrid node must reject Hysteria2")
	}
}

func TestUpdateClientProfileRejectsHysteria2OnHybridBeforeSave(t *testing.T) {
	repo := &topologyAccountRepository{
		fakeAccountRepository: &fakeAccountRepository{
			profile: SubscriptionProfile{
				Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, ServerID: "server-1"},
				Server:  &SubscriptionServer{ID: "server-1", Name: "Hybrid", VPNProtocol: ClientProtocolVLESS},
			},
		},
		deploymentRole: "hybrid",
	}
	handler := &Handler{logger: newTestHandler(repo.fakeAccountRepository).logger, accounts: repo}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/vpn-accounts/account-1/client-profile", strings.NewReader(`{
		"name":"Default",
		"clientType":"other",
		"deviceType":"other",
		"fingerprintMode":"auto",
		"fingerprint":"firefox",
		"serverNameOverride":"",
		"spiderX":"/",
		"mtu":null,
		"protocol":"hysteria2"
	}`))
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.UpdateClientProfile(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	if repo.savedProfile {
		t.Fatal("client profile must not be saved when topology is incompatible")
	}
	if repo.validatedServer != "server-1" || repo.validatedProto != ClientProtocolHysteria2 {
		t.Fatalf("unexpected topology preflight server=%q protocol=%q", repo.validatedServer, repo.validatedProto)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != "client_connection_unavailable" || !strings.Contains(body.Message, "dedicated VPN Node") {
		t.Fatalf("unexpected domain error: %+v", body)
	}
}

func TestGetClientConnectionRejectsPersistedHysteria2OnHybrid(t *testing.T) {
	repo := &topologyAccountRepository{
		fakeAccountRepository: &fakeAccountRepository{
			profile: SubscriptionProfile{
				Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, ServerID: "server-1"},
				Server:  &SubscriptionServer{ID: "server-1", Name: "Hybrid", VPNProtocol: ClientProtocolVLESS},
			},
		},
		clientProfile: ClientProfile{
			ID: "profile-1", VPNAccountID: "account-1", Name: "Default",
			ClientType: "other", DeviceType: "other", FingerprintMode: FingerprintModeAuto,
			Fingerprint: DefaultAutoFingerprint, SpiderX: "/", Protocol: ClientProtocolHysteria2,
		},
		deploymentRole: "hybrid",
	}
	handler := &Handler{logger: newTestHandler(repo.fakeAccountRepository).logger, accounts: repo}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/client-connection", nil)
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.GetClientConnection(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	if repo.validatedProto != ClientProtocolHysteria2 {
		t.Fatalf("validated protocol = %q, want hysteria2", repo.validatedProto)
	}
}
