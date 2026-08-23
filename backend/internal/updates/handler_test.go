package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/buildinfo"
)

const testJobID = "11111111-1111-4111-8111-111111111111"
const testUserID = "22222222-2222-4222-8222-222222222222"

type fakeJobRepository struct {
	job          Job
	list         []Job
	listLimit    int
	getCalled    bool
	failCode     string
	createErr    error
	markErr      error
	completeErr  error
	getErr       error
	listErr      error
}

func newFakeJobRepository() *fakeJobRepository {
	return &fakeJobRepository{job: Job{
		ID:              testJobID,
		Operation:       OperationPreflight,
		Status:          StatusPending,
		Stage:           StagePreflight,
		RequestPayload:  json.RawMessage(`{}`),
		ResultPayload:   json.RawMessage(`{}`),
		CreatedByUserID: testUserID,
	}}
}

func (r *fakeJobRepository) CreatePreflight(context.Context, string) (Job, error) {
	if r.createErr != nil {
		return Job{}, r.createErr
	}
	r.job.Status = StatusPending
	return r.job, nil
}

func (r *fakeJobRepository) MarkRunning(context.Context, string) (Job, error) {
	if r.markErr != nil {
		return Job{}, r.markErr
	}
	r.job.Status = StatusRunning
	return r.job, nil
}

func (r *fakeJobRepository) CompletePreflight(_ context.Context, _ string, result PreflightResult) (Job, error) {
	if r.completeErr != nil {
		return Job{}, r.completeErr
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	r.job.Status = StatusSucceeded
	r.job.ResultPayload = payload
	return r.job, nil
}

func (r *fakeJobRepository) Fail(_ context.Context, _ string, errorCode string) (Job, error) {
	r.failCode = errorCode
	r.job.Status = StatusFailed
	r.job.ErrorCode = errorCode
	return r.job, nil
}

func (r *fakeJobRepository) Get(context.Context, string) (Job, error) {
	r.getCalled = true
	if r.getErr != nil {
		return Job{}, r.getErr
	}
	return r.job, nil
}

func (r *fakeJobRepository) List(_ context.Context, limit int) ([]Job, error) {
	r.listLimit = limit
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.list, nil
}

type fakeSchemaReader struct {
	applied string
	err     error
}

func (r fakeSchemaReader) AppliedSchemaVersion(context.Context) (string, error) {
	return r.applied, r.err
}

type fakeAuditRecorder struct {
	events []audit.EventInput
}

func (r *fakeAuditRecorder) RecordSafe(_ context.Context, event audit.EventInput) {
	r.events = append(r.events, event)
}

func authenticatedRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	user := auth.AuthenticatedUser{UserProfile: auth.UserProfile{ID: testUserID}}
	return req.WithContext(auth.ContextWithUser(req.Context(), user))
}

func TestCreatePreflightCompletesAndAudits(t *testing.T) {
	repo := newFakeJobRepository()
	recorder := &fakeAuditRecorder{}
	handler := NewHandlerWithDependencies(nil, repo, fakeSchemaReader{applied: "000135_update_jobs"}, recorder, releaseInfo)

	response := httptest.NewRecorder()
	handler.CreatePreflight(response, authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/preflight"))

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body CreateResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Job.Status != StatusSucceeded {
		t.Fatalf("job status = %q", body.Job.Status)
	}
	var result PreflightResult
	if err := json.Unmarshal(body.Job.ResultPayload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionProceed {
		t.Fatalf("decision = %q", result.Decision)
	}
	if len(recorder.events) != 2 {
		t.Fatalf("audit events = %d, want 2", len(recorder.events))
	}
	if recorder.events[0].Action != "update.preflight.requested" || recorder.events[1].Action != "update.preflight.completed" {
		t.Fatalf("unexpected audit actions: %#v", recorder.events)
	}
}

func TestCreatePreflightBlockedIsSuccessfulJobResult(t *testing.T) {
	repo := newFakeJobRepository()
	info := releaseInfo()
	info.ExpectedDatabaseSchemaVersion = 135
	handler := NewHandlerWithDependencies(nil, repo, fakeSchemaReader{applied: "000134_distinct_tcp_listener_ports"}, nil, func() buildinfo.Info { return info })

	response := httptest.NewRecorder()
	handler.CreatePreflight(response, authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/preflight"))

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.job.Status != StatusSucceeded {
		t.Fatalf("job status = %q, want succeeded", repo.job.Status)
	}
	var result PreflightResult
	if err := json.Unmarshal(repo.job.ResultPayload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionBlocked {
		t.Fatalf("decision = %q, want blocked", result.Decision)
	}
}

func TestCreatePreflightReaderFailurePersistsFailedJob(t *testing.T) {
	repo := newFakeJobRepository()
	recorder := &fakeAuditRecorder{}
	handler := NewHandlerWithDependencies(nil, repo, fakeSchemaReader{err: errors.New("schema unavailable")}, recorder, releaseInfo)

	response := httptest.NewRecorder()
	handler.CreatePreflight(response, authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/preflight"))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.job.Status != StatusFailed || repo.failCode != "database_preflight_failed" {
		t.Fatalf("failed job = %+v, code = %q", repo.job, repo.failCode)
	}
	if len(recorder.events) != 2 || recorder.events[1].Action != "update.preflight.failed" {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}

func TestGetRejectsMalformedJobIDBeforeRepository(t *testing.T) {
	repo := newFakeJobRepository()
	handler := NewHandlerWithDependencies(nil, repo, fakeSchemaReader{}, nil, releaseInfo)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/update-jobs/not-a-uuid", nil)
	request.SetPathValue("job_id", "not-a-uuid")
	response := httptest.NewRecorder()
	handler.Get(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.getCalled {
		t.Fatal("repository Get was called for malformed UUID")
	}
}

func TestGetReturnsNotFound(t *testing.T) {
	repo := newFakeJobRepository()
	repo.getErr = pgx.ErrNoRows
	handler := NewHandlerWithDependencies(nil, repo, fakeSchemaReader{}, nil, releaseInfo)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/update-jobs/"+testJobID, nil)
	request.SetPathValue("job_id", testJobID)
	response := httptest.NewRecorder()
	handler.Get(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestListValidatesAndForwardsLimit(t *testing.T) {
	repo := newFakeJobRepository()
	handler := NewHandlerWithDependencies(nil, repo, fakeSchemaReader{}, nil, releaseInfo)

	badResponse := httptest.NewRecorder()
	handler.List(badResponse, httptest.NewRequest(http.MethodGet, "/api/v1/system/update-jobs?limit=101", nil))
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status = %d", badResponse.Code)
	}

	response := httptest.NewRecorder()
	handler.List(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/update-jobs?limit=7", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.listLimit != 7 {
		t.Fatalf("repository limit = %d, want 7", repo.listLimit)
	}
}
