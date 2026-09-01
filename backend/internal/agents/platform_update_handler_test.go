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

const (
	platformUpdateTestServerID = "550e8400-e29b-41d4-a716-446655440001"
	platformUpdateTestJobID    = "550e8400-e29b-41d4-a716-446655440000"
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
		ID: platformUpdateTestJobID, ServerID: platformUpdateTestServerID,
		TargetVersion: "v1.2.3", Status: AgentOperationJobStatusPending,
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+platformUpdateTestServerID+"/software-updates", strings.NewReader(`{"targetVersion":"v1.2.3"}`))
	request.SetPathValue("server_id", platformUpdateTestServerID)
	response := httptest.NewRecorder()

	handler.CreatePlatformUpdate(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if repository.createInput != (CreatePlatformUpdateJobInput{ServerID: platformUpdateTestServerID, TargetVersion: "v1.2.3"}) {
		t.Fatalf("unexpected create input: %+v", repository.createInput)
	}
}

func TestCreatePlatformUpdateRejectsMalformedServerIDBeforeRepository(t *testing.T) {
	repository := newPlatformUpdateAwareFakeRepository()
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/not-a-uuid/software-updates", strings.NewReader(`{"targetVersion":"v1.2.3"}`))
	request.SetPathValue("server_id", "not-a-uuid")
	response := httptest.NewRecorder()

	handler.CreatePlatformUpdate(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_server_id") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.createInput != (CreatePlatformUpdateJobInput{}) {
		t.Fatalf("malformed server ID reached repository: %+v", repository.createInput)
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
		request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+platformUpdateTestServerID+"/software-updates", strings.NewReader(body))
		request.SetPathValue("server_id", platformUpdateTestServerID)
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
		request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+platformUpdateTestServerID+"/software-updates", strings.NewReader(body))
		request.SetPathValue("server_id", platformUpdateTestServerID)
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

func TestCreatePlatformUpdateMapsReadinessAndDurableInterlockConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{name: "not ready", err: pgx.ErrNoRows, code: "update_not_ready"},
		{name: "active or unresolved", err: &pgconn.PgError{Code: "23505"}, code: "update_blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := newPlatformUpdateAwareFakeRepository()
			repository.createErr = tc.err
			handler := testAgentHandler(repository)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+platformUpdateTestServerID+"/software-updates", strings.NewReader(`{"targetVersion":"v1.2.3"}`))
			request.SetPathValue("server_id", platformUpdateTestServerID)
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
		ID: platformUpdateTestJobID, ServerID: platformUpdateTestServerID,
		TargetVersion: "v1.2.3", Status: AgentOperationJobStatusMutationDispatched,
	}
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/"+platformUpdateTestServerID+"/software-updates/"+platformUpdateTestJobID, nil)
	request.SetPathValue("server_id", platformUpdateTestServerID)
	request.SetPathValue("job_id", platformUpdateTestJobID)
	response := httptest.NewRecorder()

	handler.GetPlatformUpdate(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.queryServer != platformUpdateTestServerID || repository.queryJob != platformUpdateTestJobID {
		t.Fatalf("unexpected query scope: server=%q job=%q", repository.queryServer, repository.queryJob)
	}
}

func TestGetPlatformUpdateRejectsMalformedPathIDsBeforeRepository(t *testing.T) {
	for _, tc := range []struct {
		name     string
		serverID string
		jobID    string
	}{
		{name: "server", serverID: "not-a-uuid", jobID: platformUpdateTestJobID},
		{name: "job", serverID: platformUpdateTestServerID, jobID: "not-a-uuid"},
		{name: "uppercase", serverID: platformUpdateTestServerID, jobID: "550E8400-E29B-41D4-A716-446655440000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := newPlatformUpdateAwareFakeRepository()
			handler := testAgentHandler(repository)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/"+tc.serverID+"/software-updates/"+tc.jobID, nil)
			request.SetPathValue("server_id", tc.serverID)
			request.SetPathValue("job_id", tc.jobID)
			response := httptest.NewRecorder()

			handler.GetPlatformUpdate(response, request)

			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "update_not_found") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if repository.queryServer != "" || repository.queryJob != "" {
				t.Fatalf("malformed path IDs reached repository: server=%q job=%q", repository.queryServer, repository.queryJob)
			}
		})
	}
}

func TestGetPlatformUpdateMapsMissingJobToNotFound(t *testing.T) {
	repository := newPlatformUpdateAwareFakeRepository()
	repository.queryErr = pgx.ErrNoRows
	handler := testAgentHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/"+platformUpdateTestServerID+"/software-updates/"+platformUpdateTestJobID, nil)
	request.SetPathValue("server_id", platformUpdateTestServerID)
	request.SetPathValue("job_id", platformUpdateTestJobID)
	response := httptest.NewRecorder()

	handler.GetPlatformUpdate(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "update_not_found") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
