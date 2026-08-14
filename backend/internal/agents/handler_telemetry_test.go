package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeTelemetryAgentRepository struct {
	fakeAgentAPIRepository
	telemetryAgentID  string
	telemetryServerID string
	telemetry         HeartbeatTelemetry
}

func (f *fakeTelemetryAgentRepository) UpsertAgentTelemetry(_ context.Context, agentID, serverID string, telemetry HeartbeatTelemetry) error {
	f.telemetryAgentID = agentID
	f.telemetryServerID = serverID
	f.telemetry = telemetry
	return nil
}

func TestHeartbeatPersistsValidTelemetry(t *testing.T) {
	repository := &fakeTelemetryAgentRepository{
		fakeAgentAPIRepository: fakeAgentAPIRepository{
			heartbeatAgent: Agent{ID: "agent-id", ServerID: "server-id"},
		},
	}
	handler := testAgentHandler(repository)
	body := `{"telemetry":{"schemaVersion":1,"collectedAt":"2026-08-14T00:00:00Z","host":{"logicalCpus":4},"vpnCore":{"type":"sing-box","installed":true,"serviceState":"active"}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer rg_agent_valid")
	response := httptest.NewRecorder()

	handler.Heartbeat(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.telemetryAgentID != "agent-id" || repository.telemetryServerID != "server-id" {
		t.Fatalf("telemetry target = %q/%q", repository.telemetryAgentID, repository.telemetryServerID)
	}
}

func TestHeartbeatRejectsUnsupportedTelemetrySchema(t *testing.T) {
	repository := &fakeTelemetryAgentRepository{}
	handler := testAgentHandler(repository)
	body := `{"telemetry":{"schemaVersion":2,"collectedAt":"2026-08-14T00:00:00Z","host":{},"vpnCore":{"type":"sing-box","installed":true,"serviceState":"active"}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer rg_agent_valid")
	response := httptest.NewRecorder()

	handler.Heartbeat(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if repository.heartbeatInput.TokenHash != "" {
		t.Fatalf("invalid telemetry reached heartbeat repository: %+v", repository.heartbeatInput)
	}
}
