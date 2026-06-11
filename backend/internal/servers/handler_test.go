package servers

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

	"github.com/artuazh/routegate/backend/internal/agents"
)

type fakeServerRepository struct {
	createInput  CreateServerInput
	created      Server
	list         []ServerWithAgent
	getByID      Server
	getWithAgent ServerWithAgent
	updated      Server
	updateInput  UpdateServerInput
	deletedID    string
}

func (f *fakeServerRepository) CreateServer(_ context.Context, input CreateServerInput) (Server, error) {
	f.createInput = input
	return f.created, nil
}

func (f *fakeServerRepository) ListServersWithAgent(context.Context, ServerFilter) ([]ServerWithAgent, error) {
	return f.list, nil
}

func (f *fakeServerRepository) GetServerByID(context.Context, string) (Server, error) {
	return f.getByID, nil
}

func (f *fakeServerRepository) GetServerWithAgent(context.Context, string) (ServerWithAgent, error) {
	return f.getWithAgent, nil
}

func (f *fakeServerRepository) UpdateServer(_ context.Context, _ string, input UpdateServerInput) (Server, error) {
	f.updateInput = input
	return f.updated, nil
}

func (f *fakeServerRepository) DeleteServer(_ context.Context, id string) error {
	f.deletedID = id
	return nil
}

type fakeRegistrationTokenRepository struct {
	input agents.CreateRegistrationTokenInput
}

func (f *fakeRegistrationTokenRepository) CreateRegistrationToken(_ context.Context, input agents.CreateRegistrationTokenInput) (agents.ServerRegistrationToken, error) {
	f.input = input
	return agents.ServerRegistrationToken{
		ServerID:  input.ServerID,
		TokenHash: input.TokenHash,
		ExpiresAt: input.ExpiresAt,
	}, nil
}

func TestCreateServerMapsAdminRequest(t *testing.T) {
	repository := &fakeServerRepository{created: Server{ID: "server-id", Name: "fi-01", Status: StatusPending}}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers", strings.NewReader(`{
		"name":"  fi-01  ",
		"description":"Finland VPS",
		"location":"Finland",
		"provider":"Hostkey",
		"publicIp":"194.164.235.101",
		"privateIp":""
	}`))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if repository.createInput.Name != "fi-01" {
		t.Fatalf("name = %q, want fi-01", repository.createInput.Name)
	}
	if repository.createInput.Description != "Finland VPS" || repository.createInput.PrivateIP != "" {
		t.Fatalf("unexpected create input: %+v", repository.createInput)
	}
	if repository.createInput.Status != "" {
		t.Fatalf("status = %q, want empty so repository applies pending default", repository.createInput.Status)
	}
}

func TestUpdateServerPreservesOmittedFields(t *testing.T) {
	repository := &fakeServerRepository{updated: Server{ID: "server-id", Name: "renamed", Status: StatusActive}}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/server-id", strings.NewReader(`{"name":" renamed ","status":"active"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.Update(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.updateInput.Name == nil || *repository.updateInput.Name != "renamed" {
		t.Fatalf("name pointer was not populated and trimmed: %+v", repository.updateInput.Name)
	}
	if repository.updateInput.Description != nil || repository.updateInput.PublicIP != nil {
		t.Fatalf("omitted fields must remain nil: %+v", repository.updateInput)
	}
}

func TestCreateRegistrationTokenStoresOnlyHash(t *testing.T) {
	serverRepository := &fakeServerRepository{getByID: Server{ID: "server-id"}}
	tokenRepository := &fakeRegistrationTokenRepository{}
	handler := testHandler(serverRepository, tokenRepository)
	fixedNow := time.Date(2026, time.June, 11, 12, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return fixedNow }
	handler.generateRegistrationToken = func() (string, error) { return "rg_reg_raw-secret", nil }
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/registration-token", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreateRegistrationToken(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if tokenRepository.input.TokenHash != agents.HashToken("rg_reg_raw-secret") {
		t.Fatalf("stored token hash = %q, want SHA-256 hash", tokenRepository.input.TokenHash)
	}
	if tokenRepository.input.TokenHash == "rg_reg_raw-secret" {
		t.Fatal("raw registration token was passed to the repository")
	}
	wantExpiry := fixedNow.Add(24 * time.Hour)
	if tokenRepository.input.ExpiresAt == nil || !tokenRepository.input.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expires at = %v, want %v", tokenRepository.input.ExpiresAt, wantExpiry)
	}

	var payload RegistrationTokenResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.RegistrationToken != "rg_reg_raw-secret" {
		t.Fatalf("response token = %q, want raw one-time token", payload.RegistrationToken)
	}
	if payload.ServerID != "server-id" || !payload.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("unexpected response: %+v", payload)
	}
}

func testHandler(servers serverRepository, tokens registrationTokenRepository) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		servers:                   servers,
		registrationTokens:        tokens,
		generateRegistrationToken: agents.GenerateRegistrationToken,
		now:                       time.Now,
	}
}

func TestListFlattensServerFieldsAndIncludesAgent(t *testing.T) {
	repository := &fakeServerRepository{
		list: []ServerWithAgent{{
			Server: Server{ID: "server-id", Name: "fi-01", Status: StatusActive},
			Agent: &agents.Agent{
				ID:           "agent-id",
				Hostname:     "fi-01.example",
				AgentVersion: "1.2.3",
				Status:       agents.StatusOnline,
				Capabilities: agents.Capabilities{"wireguard": true},
			},
		}},
	}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
	response := httptest.NewRecorder()

	handler.List(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var payload map[string][]map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items := payload["items"]
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	if items[0]["id"] != "server-id" || items[0]["name"] != "fi-01" {
		t.Fatalf("server fields are not at the item top level: %v", items[0])
	}
	if _, nested := items[0]["server"]; nested {
		t.Fatalf("response unexpectedly nests server fields: %v", items[0])
	}
	agent, ok := items[0]["agent"].(map[string]any)
	if !ok || agent["id"] != "agent-id" || agent["agentVersion"] != "1.2.3" {
		t.Fatalf("agent response is missing expected fields: %v", items[0]["agent"])
	}
}

func TestGetFlattensServerFieldsWithoutAgent(t *testing.T) {
	repository := &fakeServerRepository{
		getWithAgent: ServerWithAgent{Server: Server{ID: "server-id", Name: "fi-01", Status: StatusPending}},
	}
	handler := testHandler(repository, &fakeRegistrationTokenRepository{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.Get(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["id"] != "server-id" || payload["name"] != "fi-01" {
		t.Fatalf("server fields are not at the response top level: %v", payload)
	}
	if _, nested := payload["server"]; nested {
		t.Fatalf("response unexpectedly nests server fields: %v", payload)
	}
	if _, hasAgent := payload["agent"]; hasAgent {
		t.Fatalf("response must omit an absent agent: %v", payload)
	}
}
