package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type operationAwareFakeRepository struct {
	*fakeAgentAPIRepository
	operationTask          *AgentConfigTask
	operationClaimHash     string
	operationClaimErr      error
	operationCompleteInput CompleteAgentOperationJobInput
	operationCompleteKind  string
	operationCompleteErr   error
	operationCreateInput   CreateAgentOperationJobInput
	operationCreatedTask   AgentConfigTask
	operationCreateErr     error
	operationQueryTask     AgentConfigTask
	operationQueryErr      error
}

func newOperationAwareFakeRepository() *operationAwareFakeRepository {
	return &operationAwareFakeRepository{fakeAgentAPIRepository: &fakeAgentAPIRepository{}}
}

func (f *operationAwareFakeRepository) ClaimNextAgentOperationTask(_ context.Context, tokenHash string) (*AgentConfigTask, error) {
	f.operationClaimHash = tokenHash
	return f.operationTask, f.operationClaimErr
}

func (f *operationAwareFakeRepository) CompleteAgentOperationTask(_ context.Context, input CompleteAgentOperationJobInput) (string, error) {
	f.operationCompleteInput = input
	if f.operationCompleteKind == "" {
		f.operationCompleteKind = AgentTaskKindVPNCoreService
	}
	return f.operationCompleteKind, f.operationCompleteErr
}

func (f *operationAwareFakeRepository) CreateAgentOperationJob(_ context.Context, input CreateAgentOperationJobInput) (AgentConfigTask, error) {
	f.operationCreateInput = input
	return f.operationCreatedTask, f.operationCreateErr
}

func (f *operationAwareFakeRepository) GetAgentOperationJob(context.Context, string, string) (AgentConfigTask, error) {
	return f.operationQueryTask, f.operationQueryErr
}

func TestNextTaskPrefersVPNCoreOperationQueue(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationTask = &AgentConfigTask{
		ID:        "operation-job",
		Kind:      AgentTaskKindVPNCoreService,
		ServerID:  "server-id",
		AgentID:   "agent-id",
		Operation: VPNCoreOperationRestart,
		Status:    AgentOperationJobStatusInProgress,
	}
	repository.claimedTask = &AgentConfigTask{ID: "config-job"}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/tasks/next", nil)
	request.Header.Set("Authorization", "Bearer rg_agent_valid")
	response := httptest.NewRecorder()

	handler.NextTask(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.operationClaimHash != HashToken("rg_agent_valid") {
		t.Fatalf("operation claim hash = %q", repository.operationClaimHash)
	}
	if repository.claimedTokenHash != "" {
		t.Fatalf("config queue was queried after operation claim: %q", repository.claimedTokenHash)
	}
	var payload AgentNextTaskResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Task == nil || payload.Task.ID != "operation-job" || payload.Task.Operation != VPNCoreOperationRestart {
		t.Fatalf("unexpected task: %+v", payload.Task)
	}
}

func TestCompleteTaskCompletesVPNCoreOperationWithoutConfigFallback(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/tasks/operation-job/result", strings.NewReader(`{"status":"succeeded","resultPayload":{"operation":"restart"}}`))
	request.SetPathValue("job_id", "operation-job")
	request.Header.Set("Authorization", "Bearer rg_agent_valid")
	response := httptest.NewRecorder()

	handler.CompleteTask(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.operationCompleteInput.JobID != "operation-job" || repository.operationCompleteInput.TokenHash != HashToken("rg_agent_valid") {
		t.Fatalf("unexpected operation completion: %+v", repository.operationCompleteInput)
	}
	if repository.completeInput.JobID != "" {
		t.Fatalf("config completion fallback was called: %+v", repository.completeInput)
	}
}

func TestCreateVPNCoreOperationAcceptsAllowedOperation(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationCreatedTask = AgentConfigTask{
		ID:        "operation-job",
		Kind:      AgentTaskKindVPNCoreService,
		ServerID:  "server-id",
		Operation: VPNCoreOperationStart,
		Status:    AgentOperationJobStatusPending,
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/vpn-core/operations", strings.NewReader(`{"operation":"start"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreateVPNCoreOperation(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if repository.operationCreateInput.ServerID != "server-id" || repository.operationCreateInput.Operation != VPNCoreOperationStart {
		t.Fatalf("unexpected create input: %+v", repository.operationCreateInput)
	}
}

func TestCreateVPNCoreOperationRejectsUnknownOperation(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/vpn-core/operations", strings.NewReader(`{"operation":"reload-or-run-shell"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreateVPNCoreOperation(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if repository.operationCreateInput.Operation != "" {
		t.Fatalf("unknown operation reached repository: %+v", repository.operationCreateInput)
	}
}

func TestCreateVPNCoreInstallationUsesFixedAllowListedTask(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationCreatedTask = AgentConfigTask{
		ID: "install-job", Kind: AgentTaskKindVPNCoreInstall,
		ServerID: "server-id", Operation: VPNCoreOperationInstallSingBox,
		Status: AgentOperationJobStatusPending,
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/vpn-core/installations", strings.NewReader(`{"package":"xray","command":"sh -c anything"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreateVPNCoreInstallation(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if repository.operationCreateInput != (CreateAgentOperationJobInput{
		ServerID: "server-id", Kind: AgentTaskKindVPNCoreInstall, Operation: VPNCoreOperationInstallSingBox,
	}) {
		t.Fatalf("request data crossed Manager allow-list boundary: %+v", repository.operationCreateInput)
	}
}

func TestCreateVPNCoreInstallationRejectsUnsupportedAgent(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationCreateErr = pgx.ErrNoRows
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/vpn-core/installations", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreateVPNCoreInstallation(response, request)

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "agent_installation_unsupported") {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateVPNCoreInstallationRejectsDuplicateActiveJob(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationCreateErr = &pgconn.PgError{Code: "23505"}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/vpn-core/installations", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreateVPNCoreInstallation(response, request)

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "operation_in_progress") {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGetVPNCoreInstallationRejectsServiceJob(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationQueryTask = AgentConfigTask{ID: "service-job", Kind: AgentTaskKindVPNCoreService}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/vpn-core/installations/service-job", nil)
	request.SetPathValue("server_id", "server-id")
	request.SetPathValue("job_id", "service-job")
	response := httptest.NewRecorder()

	handler.GetVPNCoreInstallation(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestGetVPNCoreInstallationSanitizesAgentResult(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationQueryTask = AgentConfigTask{
		ID: "install-job", Kind: AgentTaskKindVPNCoreInstall,
		Operation: VPNCoreOperationInstallSingBox, ErrorMessage: "secret package output",
		ResultPayload: map[string]any{
			"kind": AgentTaskKindVPNCoreInstall, "operation": VPNCoreOperationInstallSingBox,
			"status": "failed", "output": "apt output with credentials", "url": "https://untrusted.example",
			"command":  "sh -c arbitrary",
			"platform": map[string]any{"id": "debian", "version": "12", "architecture": "amd64", "secret": "hidden"},
			"stages":   []any{map[string]any{"stage": "install_package", "status": "failed", "code": "package_installation_failed", "output": "hidden"}},
		},
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/vpn-core/installations/install-job", nil)
	request.SetPathValue("server_id", "server-id")
	request.SetPathValue("job_id", "install-job")
	response := httptest.NewRecorder()

	handler.GetVPNCoreInstallation(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"credentials", "untrusted.example", "sh -c", `"secret"`, `"output"`, "secret package output"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("unsafe installation result exposed %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"errorMessage":"installation_failed"`) {
		t.Fatalf("safe error code missing: %s", body)
	}
}
