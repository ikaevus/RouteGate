package servers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateProtocolSettingsAllowsHysteria2OnHybridNodeWithDedicatedHostname(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{ServerID: "server-id", Protocol: "hysteria2"}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{
		getByID: Server{ID: "server-id", DeploymentRole: "hybrid", Hostname: "manager.example.com"},
	}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{
		"protocol":"hysteria2",
		"hysteria2Port":443,
		"hysteria2Domain":"vpn.example.com",
		"hysteria2AcmeEmail":"admin@example.com",
		"hysteria2MasqueradeUrl":"https://www.cloudflare.com/"
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fakeProtocolSettingsInput.Protocol == nil || *fakeProtocolSettingsInput.Protocol != "hysteria2" {
		t.Fatalf("protocol input = %+v, want hysteria2", fakeProtocolSettingsInput.Protocol)
	}
}

func TestUpdateProtocolSettingsRejectsManagerHostnameForHybridHysteria2(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{ServerID: "server-id", Protocol: "vless"}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{getByID: Server{ID: "server-id", DeploymentRole: "hybrid", Hostname: "manager.example.com"}}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{
		"protocol":"hysteria2",
		"hysteria2Port":443,
		"hysteria2Domain":"manager.example.com",
		"hysteria2AcmeEmail":"admin@example.com",
		"hysteria2MasqueradeUrl":"https://www.cloudflare.com/"
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "different from the Manager hostname") {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if fakeProtocolSettingsInput.Protocol != nil {
		t.Fatalf("repository update was called for an unsafe hostname: %+v", fakeProtocolSettingsInput)
	}
}

func TestUpdateProtocolSettingsAllowsSelectingHybridHysteria2WithExistingDedicatedHostname(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{
		ServerID: "server-id", Protocol: "vless", Hysteria2Domain: "hy2.example.com",
	}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{getByID: Server{ID: "server-id", DeploymentRole: "hybrid", Hostname: "manager.example.com"}}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{"protocol":"hysteria2"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fakeProtocolSettingsInput.Protocol == nil || *fakeProtocolSettingsInput.Protocol != "hysteria2" {
		t.Fatalf("protocol input = %+v, want hysteria2", fakeProtocolSettingsInput.Protocol)
	}
}

func TestUpdateProtocolSettingsRejectsManagerHostnamePatchForActiveHybridHysteria2(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{
		ServerID: "server-id", Protocol: "hysteria2", Hysteria2Domain: "hy2.example.com",
	}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{getByID: Server{ID: "server-id", DeploymentRole: "hybrid", Hostname: "manager.example.com"}}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{"hysteria2Domain":"manager.example.com"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "different from the Manager hostname") {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if fakeProtocolSettingsInput.Hysteria2Domain != nil {
		t.Fatalf("repository update was called for an unsafe hostname: %+v", fakeProtocolSettingsInput)
	}
}

func TestUpdateProtocolSettingsRejectsManagerHostnameForSecondaryHybridHysteria2(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{
		ServerID: "server-id", Protocol: "vless",
	}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{getByID: Server{ID: "server-id", DeploymentRole: "hybrid", Hostname: "manager.example.com"}}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{
		"protocol":"vless",
		"hysteria2Domain":"manager.example.com",
		"hysteria2AcmeEmail":"admin@example.com"
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "different from the Manager hostname") {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if fakeProtocolSettingsInput.Hysteria2Domain != nil {
		t.Fatalf("repository update was called for an unsafe secondary Hysteria2 hostname: %+v", fakeProtocolSettingsInput)
	}
}

func TestUpdateProtocolSettingsAllowsHysteria2OnDedicatedVPNNode(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{ServerID: "server-id", Protocol: "hysteria2"}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{
		getByID: Server{ID: "server-id", DeploymentRole: "vpn"},
	}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{
		"protocol":"hysteria2",
		"hysteria2Port":443,
		"hysteria2Domain":"vpn.example.com",
		"hysteria2AcmeEmail":"admin@example.com",
		"hysteria2MasqueradeUrl":"https://www.cloudflare.com/"
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fakeProtocolSettingsInput.Protocol == nil || *fakeProtocolSettingsInput.Protocol != "hysteria2" {
		t.Fatalf("protocol input = %+v, want hysteria2", fakeProtocolSettingsInput.Protocol)
	}
}
