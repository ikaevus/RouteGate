package updates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDiscoveryRepository struct {
	job         Job
	createErr   error
	markErr     error
	completeErr error
	failCode    string
}

func newFakeDiscoveryRepository() *fakeDiscoveryRepository {
	return &fakeDiscoveryRepository{job: Job{
		ID:             testJobID,
		Operation:      OperationDiscovery,
		Status:         StatusPending,
		Stage:          StageDiscovery,
		RequestPayload: json.RawMessage(`{}`),
		ResultPayload:  json.RawMessage(`{}`),
	}}
}

func (r *fakeDiscoveryRepository) CreateDiscovery(context.Context, string) (Job, error) {
	if r.createErr != nil {
		return r.job, r.createErr
	}
	r.job.Status = StatusPending
	return r.job, nil
}

func (r *fakeDiscoveryRepository) MarkRunning(context.Context, string) (Job, error) {
	if r.markErr != nil {
		return Job{}, r.markErr
	}
	r.job.Status = StatusRunning
	return r.job, nil
}

func (r *fakeDiscoveryRepository) CompleteDiscovery(_ context.Context, _ string, result DiscoveryResult) (Job, error) {
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

func (r *fakeDiscoveryRepository) Fail(_ context.Context, _ string, errorCode string) (Job, error) {
	r.failCode = errorCode
	r.job.Status = StatusFailed
	r.job.ErrorCode = errorCode
	return r.job, nil
}

type fakeReleaseDiscoverer struct {
	result DiscoveryResult
	err    error
	called int
}

func (d *fakeReleaseDiscoverer) Discover(context.Context, string, string, string) (DiscoveryResult, error) {
	d.called++
	return d.result, d.err
}

func discoveryHandler(repo *fakeDiscoveryRepository, discoverer *fakeReleaseDiscoverer, recorder auditRecorder) *Handler {
	handler := NewHandlerWithDependencies(nil, newFakeJobRepository(), fakeSchemaReader{}, recorder, releaseInfo)
	handler.discoveryRepo = repo
	handler.discoverer = discoverer
	handler.runtimeOS = "linux"
	handler.runtimeArch = "amd64"
	return handler
}

func TestCreateDiscoveryCompletesAndAudits(t *testing.T) {
	repo := newFakeDiscoveryRepository()
	discoverer := &fakeReleaseDiscoverer{result: DiscoveryResult{
		Source:               DiscoverySourceOfficialGitHub,
		CurrentVersion:       "v0.2.0",
		CandidateVersion:     "v0.3.0",
		PublishedAt:          "2026-08-24T00:00:00Z",
		RuntimeOS:            "linux",
		RuntimeArch:          "amd64",
		Assets:               []DiscoveryAsset{{Name: "release-manifest.json", Size: 10}},
		MissingAssets:        []string{},
		Availability:         AvailabilityUpdateAvailable,
		ProvenanceStatus:     ProvenanceUnverified,
		VerificationRequired: ProvenanceVerificationRG96B,
	}}
	recorder := &fakeAuditRecorder{}
	handler := discoveryHandler(repo, discoverer, recorder)

	response := httptest.NewRecorder()
	handler.CreateDiscovery(response, authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/discovery"))

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.job.Status != StatusSucceeded || discoverer.called != 1 {
		t.Fatalf("job = %+v, discovery calls = %d", repo.job, discoverer.called)
	}
	if len(recorder.events) != 2 || recorder.events[0].Action != "update.discovery.requested" || recorder.events[1].Action != "update.discovery.completed" {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
	if recorder.events[1].Metadata["availability"] != AvailabilityUpdateAvailable || recorder.events[1].Metadata["candidate_version"] != "v0.3.0" {
		t.Fatalf("unexpected audit metadata: %#v", recorder.events[1].Metadata)
	}
}

func TestCreateDiscoveryExternalFailurePersistsSafeFailedJob(t *testing.T) {
	repo := newFakeDiscoveryRepository()
	discoverer := &fakeReleaseDiscoverer{err: errors.New("remote response contained secret-looking text")}
	recorder := &fakeAuditRecorder{}
	handler := discoveryHandler(repo, discoverer, recorder)

	response := httptest.NewRecorder()
	handler.CreateDiscovery(response, authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/discovery"))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.job.Status != StatusFailed || repo.failCode != discoveryExternalFailureCode {
		t.Fatalf("job = %+v, fail code = %q", repo.job, repo.failCode)
	}
	if strings.Contains(response.Body.String(), "secret-looking") {
		t.Fatalf("raw remote error leaked in response: %s", response.Body.String())
	}
	if len(recorder.events) != 2 || recorder.events[1].Metadata["error_code"] != discoveryExternalFailureCode {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}

func TestCreateDiscoveryRejectsRequestParametersBeforeJobCreation(t *testing.T) {
	repo := newFakeDiscoveryRepository()
	discoverer := &fakeReleaseDiscoverer{}
	handler := discoveryHandler(repo, discoverer, nil)

	request := authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/discovery")
	request.Body = io.NopCloser(strings.NewReader(`{"source":"https://example.com"}`))
	request.ContentLength = int64(len(`{"source":"https://example.com"}`))
	response := httptest.NewRecorder()
	handler.CreateDiscovery(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if discoverer.called != 0 || repo.job.Status != StatusPending {
		t.Fatalf("request reached discovery pipeline: job=%+v calls=%d", repo.job, discoverer.called)
	}
}

func TestCreateDiscoveryAmbiguousInsertReconcilesPersistedJob(t *testing.T) {
	repo := newFakeDiscoveryRepository()
	repo.createErr = context.DeadlineExceeded
	recorder := &fakeAuditRecorder{}
	handler := discoveryHandler(repo, &fakeReleaseDiscoverer{}, recorder)

	response := httptest.NewRecorder()
	handler.CreateDiscovery(response, authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/discovery"))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.job.Status != StatusFailed || repo.failCode != discoveryInsertAmbiguousCode {
		t.Fatalf("reconciled job = %+v, code = %q", repo.job, repo.failCode)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != "update.discovery.failed" {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}
