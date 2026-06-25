package servers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var fakeProtocolSettingsResult ProtocolSettings
var fakeProtocolSettingsInput UpdateProtocolSettingsInput

func (f *fakeServerRepository) GetProtocolSettings(context.Context, string) (ProtocolSettings, error) {
	return fakeProtocolSettingsResult, nil
}

func (f *fakeServerRepository) UpdateProtocolSettings(_ context.Context, serverID string, input UpdateProtocolSettingsInput) (ProtocolSettings, error) {
	fakeProtocolSettingsInput = input
	result := fakeProtocolSettingsResult
	result.ServerID = serverID
	if input.VLESSPort != nil {
		result.VLESSPort = *input.VLESSPort
	}
	if input.VLESSFlow != nil {
		result.VLESSFlow = *input.VLESSFlow
	}
	if input.VLESSNetwork != nil {
		result.VLESSNetwork = *input.VLESSNetwork
	}
	if input.RealityPublicKey != nil {
		result.RealityPublicKey = *input.RealityPublicKey
	}
	if input.RealityShortID != nil {
		result.RealityShortID = *input.RealityShortID
	}
	if input.RealityServerName != nil {
		result.RealityServerName = *input.RealityServerName
	}
	return result, nil
}

func TestGetProtocolSettingsReturnsVLESSRealityPayload(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{
		ServerID:          "server-id",
		VLESSPort:         443,
		VLESSFlow:         "xtls-rprx-vision",
		VLESSNetwork:      "tcp",
		RealityPublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
		RealityShortID:    "0123456789abcdef",
		RealityServerName: "www.example.com",
		UpdatedAt:         time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC),
	}
	handler := testHandler(&fakeServerRepository{}, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/protocol-settings", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.GetProtocolSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var payload ProtocolSettingsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ServerID != "server-id" || payload.Protocol != "vless" {
		t.Fatalf("unexpected identity fields: %+v", payload)
	}
	if payload.VLESS.Port != 443 || payload.VLESS.Flow != "xtls-rprx-vision" || payload.VLESS.Network != "tcp" {
		t.Fatalf("unexpected VLESS payload: %+v", payload.VLESS)
	}
	if !payload.Reality.Enabled || payload.Reality.PublicKey != "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0" {
		t.Fatalf("unexpected Reality payload: %+v", payload.Reality)
	}
}

func TestUpdateProtocolSettingsMapsTrimmedRequest(t *testing.T) {
	fakeProtocolSettingsResult = ProtocolSettings{ServerID: "server-id", UpdatedAt: time.Now().UTC()}
	fakeProtocolSettingsInput = UpdateProtocolSettingsInput{}
	handler := testHandler(&fakeServerRepository{}, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{
		"vlessPort":443,
		"vlessFlow":" xtls-rprx-vision ",
		"vlessNetwork":" tcp ",
		"realityPublicKey":" public-key ",
		"realityShortId":" 0123456789abcdef ",
		"realityServerName":" www.example.com "
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if fakeProtocolSettingsInput.VLESSPort == nil || *fakeProtocolSettingsInput.VLESSPort != 443 {
		t.Fatalf("unexpected VLESS port input: %+v", fakeProtocolSettingsInput.VLESSPort)
	}
	if fakeProtocolSettingsInput.VLESSFlow == nil || *fakeProtocolSettingsInput.VLESSFlow != "xtls-rprx-vision" {
		t.Fatalf("unexpected VLESS flow input: %+v", fakeProtocolSettingsInput.VLESSFlow)
	}
	if fakeProtocolSettingsInput.VLESSNetwork == nil || *fakeProtocolSettingsInput.VLESSNetwork != "tcp" {
		t.Fatalf("unexpected VLESS network input: %+v", fakeProtocolSettingsInput.VLESSNetwork)
	}
	if fakeProtocolSettingsInput.RealityServerName == nil || *fakeProtocolSettingsInput.RealityServerName != "www.example.com" {
		t.Fatalf("unexpected Reality server name input: %+v", fakeProtocolSettingsInput.RealityServerName)
	}
}

func TestUpdateProtocolSettingsRejectsInvalidPort(t *testing.T) {
	handler := testHandler(&fakeServerRepository{}, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id/protocol-settings", strings.NewReader(`{"vlessPort":70000}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.UpdateProtocolSettings(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}
