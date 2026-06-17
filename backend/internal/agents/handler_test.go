package agents

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeAgentAPIRepository struct {
	registrationTokens []ServerRegistrationToken
	registrationErrors []error
	registrationHashes []string
	createInput        CreateOrReplaceAgentInput
	createdAgent       Agent
	createErr          error
	activatedServerID  string
	activateErr        error
	heartbeatInput     UpdateAgentHeartbeatInput
	heartbeatAgent     Agent
	heartbeatErr       error
}

func (f *fakeAgentAPIRepository) ConsumeValidRegistrationTokenByHash(_ context.Context, hash string) (ServerRegistrationToken, error) {
	f.registrationHashes = append(f.registrationHashes, hash)
	index := len(f.registrationHashes) - 1
	if index < len(f.registrationErrors) && f.registrationErrors[index] != nil {
		return ServerRegistrationToken{}, f.registrationErrors[index]
	}
	if index < len(f.registrationTokens) {
		return f.registrationTokens[index], nil
	}
	return ServerRegistrationToken{}, pgx.ErrNoRows
}

func (f *fakeAgentAPIRepository) CreateOrReplaceAgentForServer(_ context.Context, input CreateOrReplaceAgentInput) (Agent, error) {
	f.createInput = input
	return f.createdAgent, f.createErr
}

func (f *fakeAgentAPIRepository) ActivateServer(_ context.Context, serverID string) error {
	f.activatedServerID = serverID
	return f.activateErr
}

func (f *fakeAgentAPIRepository) UpdateAgentHeartbeat(_ context.Context, input UpdateAgentHeartbeatInput) (Agent, error) {
	f.heartbeatInput = input
	return f.heartbeatAgent, f.heartbeatErr
}

func TestRegisterRejectsMissingRegistrationToken(t *testing.T) {
	handler := testAgentHandler(&fakeAgentAPIRepository{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", strings.NewReader(`{"hostname":"fi-01"}`))
	response := httptest.NewRecorder()

	handler.Register(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestRegisterRejectsInvalidExpiredOrUsedToken(t *testing.T) {
	repository := &fakeAgentAPIRepository{registrationErrors: []error{pgx.ErrNoRows}}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", strings.NewReader(`{"registrationToken":"rg_reg_invalid"}`))
	response := httptest.NewRecorder()

	handler.Register(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if repository.registrationHashes[0] != HashToken("rg_reg_invalid") {
		t.Fatalf("registration hash = %q, want hashed raw token", repository.registrationHashes[0])
	}
}

func TestRegisterCannotConsumeRegistrationTokenTwice(t *testing.T) {
	repository := &fakeAgentAPIRepository{
		registrationTokens: []ServerRegistrationToken{{ID: "registration-id", ServerID: "server-id"}},
		createdAgent:       Agent{ID: "agent-id", ServerID: "server-id"},
	}
	handler := testAgentHandler(repository)
	handler.generateAgentToken = func() (string, error) { return "rg_agent_raw-secret", nil }

	firstRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", strings.NewReader(`{"registrationToken":"rg_reg_one-time"}`))
	firstResponse := httptest.NewRecorder()
	handler.Register(firstResponse, firstRequest)
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", firstResponse.Code, http.StatusCreated, firstResponse.Body.String())
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", strings.NewReader(`{"registrationToken":"rg_reg_one-time"}`))
	secondResponse := httptest.NewRecorder()
	handler.Register(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusUnauthorized {
		t.Fatalf("second status = %d, want %d; body=%s", secondResponse.Code, http.StatusUnauthorized, secondResponse.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if payload["status"] != "invalid_registration_token" {
		t.Fatalf("second response status = %v, want invalid_registration_token", payload["status"])
	}
	if len(repository.registrationHashes) != 2 || repository.registrationHashes[0] != repository.registrationHashes[1] {
		t.Fatalf("consume hashes = %v, want two attempts for the same hashed token", repository.registrationHashes)
	}
}

func TestRegisterReturnsOneTimeAgentTokenAndStoresOnlyHash(t *testing.T) {
	fixedNow := time.Date(2026, time.June, 11, 12, 0, 0, 0, time.UTC)
	repository := &fakeAgentAPIRepository{
		registrationTokens: []ServerRegistrationToken{{ID: "registration-id", ServerID: "server-id"}},
		createdAgent:       Agent{ID: "agent-id", ServerID: "server-id"},
	}
	handler := testAgentHandler(repository)
	handler.now = func() time.Time { return fixedNow }
	handler.generateAgentToken = func() (string, error) { return "rg_agent_raw-secret", nil }
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", strings.NewReader(`{
		"registrationToken":"rg_reg_one-time",
		"hostname":" fi-01 ",
		"agentVersion":"0.1.0",
		"os":"linux",
		"arch":"amd64",
		"capabilities":{"singbox":true}
	}`))
	response := httptest.NewRecorder()

	handler.Register(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if repository.createInput.TokenHash != HashToken("rg_agent_raw-secret") {
		t.Fatalf("stored token hash = %q, want SHA-256 hash", repository.createInput.TokenHash)
	}
	if repository.createInput.TokenHash == "rg_agent_raw-secret" {
		t.Fatal("raw agent token was passed to the repository")
	}
	if repository.createInput.Status != StatusOnline || repository.createInput.LastSeenAt == nil || !repository.createInput.LastSeenAt.Equal(fixedNow) {
		t.Fatalf("registration status/last seen = %q/%v, want online/%v", repository.createInput.Status, repository.createInput.LastSeenAt, fixedNow)
	}
	if repository.activatedServerID != "server-id" {
		t.Fatalf("registration did not activate server %q", repository.activatedServerID)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["agentToken"] != "rg_agent_raw-secret" || !strings.HasPrefix(payload["agentToken"].(string), "rg_agent_") {
		t.Fatalf("agent token = %v, want generated rg_agent_ token", payload["agentToken"])
	}
	if _, exposed := payload["token_hash"]; exposed {
		t.Fatalf("response exposed token_hash: %v", payload)
	}
	if _, exposed := payload["tokenHash"]; exposed {
		t.Fatalf("response exposed tokenHash: %v", payload)
	}
	if strings.Contains(response.Body.String(), "rg_reg_one-time") {
		t.Fatal("response exposed the raw registration token")
	}
}

func TestHeartbeatRejectsMissingOrMalformedAuthorization(t *testing.T) {
	for _, header := range []string{"", "Basic secret", "Bearer", "Bearer one two"} {
		t.Run(header, func(t *testing.T) {
			handler := testAgentHandler(&fakeAgentAPIRepository{})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat", strings.NewReader(`{}`))
			request.Header.Set("Authorization", header)
			response := httptest.NewRecorder()

			handler.Heartbeat(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
		})
	}
}

func TestHeartbeatRejectsInvalidAgentToken(t *testing.T) {
	repository := &fakeAgentAPIRepository{heartbeatErr: pgx.ErrNoRows}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer rg_agent_invalid")
	response := httptest.NewRecorder()

	handler.Heartbeat(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
	if repository.heartbeatInput.TokenHash != HashToken("rg_agent_invalid") {
		t.Fatalf("heartbeat token hash = %q, want hashed bearer token", repository.heartbeatInput.TokenHash)
	}
}

func TestHeartbeatAcceptsValidBearerToken(t *testing.T) {
	repository := &fakeAgentAPIRepository{heartbeatAgent: Agent{ID: "agent-id", ServerID: "server-id"}}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/heartbeat", strings.NewReader(`{
		"agentVersion":"0.1.1",
		"capabilities":{"nftables":true}
	}`))
	request.Header.Set("Authorization", "Bearer rg_agent_valid")
	response := httptest.NewRecorder()

	handler.Heartbeat(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.heartbeatInput.TokenHash != HashToken("rg_agent_valid") {
		t.Fatalf("heartbeat token hash = %q, want hashed bearer token", repository.heartbeatInput.TokenHash)
	}
	if repository.heartbeatInput.AgentVersion == nil || *repository.heartbeatInput.AgentVersion != "0.1.1" {
		t.Fatalf("heartbeat version = %v, want 0.1.1", repository.heartbeatInput.AgentVersion)
	}
	var payload AgentHeartbeatResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || payload.AgentID != "agent-id" || payload.ServerID != "server-id" || payload.ServerStatus != activeServerStatus {
		t.Fatalf("unexpected response: %+v", payload)
	}
}

func testAgentHandler(repository agentAPIRepository) *Handler {
	return &Handler{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		repository:         repository,
		generateAgentToken: GenerateAgentToken,
		now:                time.Now,
	}
}
