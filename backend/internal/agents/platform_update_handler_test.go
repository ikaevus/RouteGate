package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type platformUpdateAwareFakeRepository struct {
	*fakeAgentAPIRepository
	createInput CreatePlatformUpdateJobInput
	createdJob PlatformUpdateJob
	createErr   error
	queryServer string
	queryJob    string
	queriedJob  PlatformUpdateJob
	queryErr    error
}

func newPlatformUpdateAwareFakeRepository() *platformUpdateAwareFakeRepository {
	return &platformUpdateAwareFakeRepository{fakeAgentAPIRepository: &fakeAgentAPIRepository{}}
}

func (f *platformUpdateAwareFakeRepository) CreatePlatformUpdateJob(_ context.Context, input CreatePlatformUpdateJobInput) (PlatformUpdateJob, error) {
	f.createInput = input
	return f.createdJob, f.createErr
}

func (f *platformUpdateAwareFakeRepository) GetPlatformUpdateJob(_ context.Context, serverID, jobID string) (PlatformUpdateJob, error) {
	f.queryServer = serverID
	f.queryJob = jobID
	return f.queriedJob, f.queryErr
}

func TestCreatePlatformUpdateAcceptsVersionOnlyRequest(t *testing.T) {
	repository := newPlatformUpdateAwareFakeRepository()
	repository.createdJob = PlatformUpdateJob{
		ID: "550e8400-e29b-41d4-a716-446655440000", ServerID: "server-id",
		TargetVersion: "v1.2.3", Status: AgentOperationJobStatusPending,
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/software-updates", strings.NewReader(`{"targetVersion":"v1.2.3"}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreatePlatformUpdate(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if repository.createInput != (CreatePlatformUpdateJobInput{ServerID: "server-id", TargetVersion: "v1.2.3"}) {
		t.Fatalf("unexpected create input: %+v", repository.createInput)
	}
}

func TestCreatePlatformUpdateRejectsPrivilegedSelectorsAndUnknownFields(t *testing.T) {
	for _, body := range []string{
		`{"targetVersion":"v1.2.3","url":"https://example.com/release"}`,
		`{"targetVersion":"v1.2.3","path":"/tmp/bundle"}`,
		`{"targetVersion":"v1.2.3","command":"sh -c id"}`,
		`{"targetVersion":"v1.2.3","role":"management"}`,
		`{"targetVersion":"v1.2.3","force":true}`,
	} {
		repository := newPlatformUpdateAwareFakeRepository()
		handler := testAgentHandler(repository)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/software-updates", strings.NewReader(body))
		request.SetPathValue("server_id", "server-id")
		response := httptest.NewRecorder()

		handler.CreatePlatformUpdate(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d want=%d response=%s", body, response.Code, http.StatusBadRequest, response.Body.String())
		}
		if repository.createInput != (CreatePlatformUpdateJobInput{}) {
			t.Fatalf("privileged selector crossed API boundary: body=%s input=%+v", body, repository.createInput)
		}
	}
}

func TestCreatePlatformUpdateRejectsNonCanonicalOrTrailingInput(t *testing.T) {
	for _, body := range []string{
		`{"targetVersion":" v1.2.3"}`,
		`{"targetVersion":"latest"}`,
		`{"targetVersion":"v1.2.3"} {"targetVersion":"v1.2.4"}`,
		`{}`,
	} {
		repository := newPlatformUpdateAwareFakeRepository()
		handler := testAgentHandler(repository)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/software-updates", strings.NewReader(body))
		request.SetPathValue("server_id", "server-id")
		response := httptest.NewRecorder()

		handler.CreatePlatformUpdate(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d want=%d response=%s", body, response.Code, http.StatusBadRequest, response.Body.String())
		}
		if repository.createInput != (CreatePlatformUpdateJobInput{}) {
			t.Fatalf("invalid request reached repository: body=%s input=%+v", body, repository.createInput)
		}
	}
}

func TestCreatePlatformUpdateMapsReadinessAndActiveJobConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{name: "not ready", err: pgx.ErrNoRows, code: "update_not_ready"},
		{name: "active job", err: &pgconn.PgError{Code: "23505"}, code: "update_in_progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := newPlatformUpdateAwareFakeRepository()
			repository.createErr = tc.err
			handler := testAgentHandler(repository)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/server-id/software-updates", strings.NewReader(`{"targetVersion":"v1.2.3"}`))
			request.SetPathValue("server_id", "server-id")
			response := httptest.NewRecorder()

			handler.CreatePlatformUpdate(response, request)

			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestGetPlatformUpdateIsServerScoped(t *testing.T) {
	repository := newPlatformUpdateAwareFakeRepository()
	repository.queriedJob = PlatformUpdateJob{
		ID: "550e8400-e29b-41d4-a716-446655440000", ServerID: "server-id",
		TargetVersion: "v1.2.3", Status: AgentOperationJobStatusMutationDispatched,
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/software-updates/550e8400-e29b-41d4-a716-446655440000", nil)
	request.SetPathValue("server_id", "server-id")
	request.SetPathValue("job_id", "550e8400-e29b-41d4-a716-446655440000")
	response := httptest.NewRecorder()

	handler.GetPlatformUpdate(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.queryServer != "server-id" || repository.queryJob != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("unexpected query scope: server=%q job=%q", repository.queryServer, repository.queryJob)
	}
}

func TestGetPlatformUpdateMapsMissingJobToNotFound(t *testing.T) {
	repository := newPlatformUpdateAwareFakeRepository()
	repository.queryErr = pgx.ErrNoRows
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/software-updates/job-id", nil)
	request.SetPathValue("server_id", "server-id")
	request.SetPathValue("job_id", "job-id")
	response := httptest.NewRecorder()

	handler.GetPlatformUpdate(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "update_not_found") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
