package servers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateProtocolSettingsRejectsHysteria2OnHybridNode(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{ServerID: "server-id", Protocol: "vless"}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	repository := &fakeServerRepository{
		getByID: Server{ID: "server-id", DeploymentRole: "hybrid"},
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

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "dedicated VPN Node") {
		t.Fatalf("response did not explain the Hysteria2 topology boundary: %s", response.Body.String())
	}
	if fakeProtocolSettingsInput.Protocol != nil {
		t.Fatalf("repository update was called for unsupported topology: %+v", fakeProtocolSettingsInput)
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
