package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type operationAwareFakeRepository struct {
	*fakeAgentAPIRepository
	operationTask          *AgentConfigTask
	operationClaimHash     string
	operationClaimErr      error
	operationCompleteInput CompleteAgentOperationJobInput
	operationCompleteErr   error
	operationCreateInput   CreateAgentOperationJobInput
	operationCreatedTask   AgentConfigTask
	operationCreateErr     error
}

func newOperationAwareFakeRepository() *operationAwareFakeRepository {
	return &operationAwareFakeRepository{fakeAgentAPIRepository: &fakeAgentAPIRepository{}}
}

func (f *operationAwareFakeRepository) ClaimNextAgentOperationTask(_ context.Context, tokenHash string) (*AgentConfigTask, error) {
	f.operationClaimHash = tokenHash
	return f.operationTask, f.operationClaimErr
}

func (f *operationAwareFakeRepository) CompleteAgentOperationTask(_ context.Context, input CompleteAgentOperationJobInput) error {
	f.operationCompleteInput = input
	return f.operationCompleteErr
}

func (f *operationAwareFakeRepository) CreateAgentOperationJob(_ context.Context, input CreateAgentOperationJobInput) (AgentConfigTask, error) {
	f.operationCreateInput = input
	return f.operationCreatedTask, f.operationCreateErr
}

func TestNextTaskPrefersVPNCoreOperationQueue(t *testing.T) {
	repository := newOperationAwareFakeRepository()
	repository.operationTask = &AgentConfigTask{
		ID: "operation-job",
		Kind: AgentTaskKindVPNCoreService,
		ServerID: "server-id",
		AgentID: "agent-id",
		Operation: VPNCoreOperationRestart,
		Status: AgentOperationJobStatusInProgress,
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
		ID: "operation-job",
		Kind: AgentTaskKindVPNCoreService,
		ServerID: "server-id",
		Operation: VPNCoreOperationStart,
		Status: AgentOperationJobStatusPending,
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
