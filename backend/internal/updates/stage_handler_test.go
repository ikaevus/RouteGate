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

const testDiscoveryJobID = "33333333-3333-4333-8333-333333333333"

type fakeStageRepository struct {
	discoveryJob          Job
	stageJob              Job
	createCalls           int
	markCalls             int
	reuseStage            bool
	completeCalls         int
	failCode              string
	createErr             error
	markErr               error
	completeErr           error
	completeCommitThenErr bool
	discoveryGetErr       error
	stageGetErr           error
}

func newFakeStageRepository(t *testing.T) *fakeStageRepository {
	t.Helper()
	bundleName := "routegate-v0.2.0-linux-amd64.tar.gz"
	contents := stageFixtureContents(bundleName)
	discovery := stageFixtureDiscovery("v0.2.0", "amd64", contents)
	payload, err := json.Marshal(discovery)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeStageRepository{
		discoveryJob: Job{
			ID:            testDiscoveryJobID,
			Operation:     OperationDiscovery,
			Status:        StatusSucceeded,
			Stage:         StageDiscovery,
			ResultPayload: payload,
		},
		stageJob: Job{
			ID:              testJobID,
			Operation:       OperationStage,
			Status:          StatusPending,
			Stage:           StageStage,
			RequestPayload:  json.RawMessage(`{"discoveryJobId":"33333333-3333-4333-8333-333333333333"}`),
			ResultPayload:   json.RawMessage(`{}`),
			CreatedByUserID: testUserID,
		},
	}
}

func (r *fakeStageRepository) CreateStage(context.Context, string, string) (Job, bool, error) {
	r.createCalls++
	if r.createErr != nil {
		return r.stageJob, false, r.createErr
	}
	if !r.reuseStage {
		r.stageJob.Status = StatusPending
	}
	return r.stageJob, r.reuseStage, nil
}

func (r *fakeStageRepository) MarkRunning(context.Context, string) (Job, error) {
	r.markCalls++
	if r.markErr != nil {
		return Job{}, r.markErr
	}
	r.stageJob.Status = StatusRunning
	return r.stageJob, nil
}

func (r *fakeStageRepository) CompleteStage(_ context.Context, _ string, result StageResult) (Job, error) {
	r.completeCalls++
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	if r.completeCommitThenErr {
		r.stageJob.Status = StatusSucceeded
		r.stageJob.ResultPayload = payload
		return Job{}, r.completeErr
	}
	if r.completeErr != nil {
		return Job{}, r.completeErr
	}
	r.stageJob.Status = StatusSucceeded
	r.stageJob.ResultPayload = payload
	return r.stageJob, nil
}

func (r *fakeStageRepository) Fail(_ context.Context, _ string, errorCode string) (Job, error) {
	r.failCode = errorCode
	r.stageJob.Status = StatusFailed
	r.stageJob.ErrorCode = errorCode
	return r.stageJob, nil
}

func (r *fakeStageRepository) Get(_ context.Context, id string) (Job, error) {
	switch id {
	case testDiscoveryJobID:
		if r.discoveryGetErr != nil {
			return Job{}, r.discoveryGetErr
		}
		return r.discoveryJob, nil
	case testJobID:
		if r.stageGetErr != nil {
			return Job{}, r.stageGetErr
		}
		return r.stageJob, nil
	default:
		return Job{}, errors.New("job not found")
	}
}

type fakeArtifactStager struct {
	result       StageResult
	err          error
	stageCalls   int
	cleanupCalls []string
	ctxErr       error
}

func (s *fakeArtifactStager) StageAndVerify(ctx context.Context, _ string, _ DiscoveryResult) (StageResult, error) {
	s.stageCalls++
	s.ctxErr = ctx.Err()
	return s.result, s.err
}

func (s *fakeArtifactStager) Cleanup(jobID string) error {
	s.cleanupCalls = append(s.cleanupCalls, jobID)
	return nil
}

func stageRequest(t *testing.T) *http.Request {
	t.Helper()
	request := authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/stage")
	body := `{"discoveryJobId":"` + testDiscoveryJobID + `"}`
	request.Body = io.NopCloser(strings.NewReader(body))
	request.ContentLength = int64(len(body))
	return request
}

func successfulStageResult() StageResult {
	return StageResult{
		CandidateVersion:  "v0.2.0",
		VerifiedVersion:   "v0.2.0",
		VerifiedCommit:    strings.Repeat("a", 40),
		ExpectedMigration: "000137_update_job_stage",
		RuntimeOS:         "linux",
		RuntimeArch:       "amd64",
		Artifact: VerifiedArtifact{
			Name:   "routegate-v0.2.0-linux-amd64.tar.gz",
			OS:     "linux",
			Arch:   "amd64",
			SHA256: strings.Repeat("b", 64),
			Size:   123,
		},
		ProvenanceStatus: ProvenanceVerified,
		Verification:     VerificationRG96C3A,
	}
}

func TestCreateStageCompletesAndAudits(t *testing.T) {
	repo := newFakeStageRepository(t)
	stager := &fakeArtifactStager{result: successfulStageResult()}
	recorder := &fakeAuditRecorder{}
	handler := newStageHandlerWithDependencies(nil, repo, recorder, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.createCalls != 1 || repo.markCalls != 1 || repo.completeCalls != 1 || stager.stageCalls != 1 {
		t.Fatalf("unexpected lifecycle calls: create=%d mark=%d complete=%d stage=%d", repo.createCalls, repo.markCalls, repo.completeCalls, stager.stageCalls)
	}
	if repo.stageJob.Status != StatusSucceeded {
		t.Fatalf("stage job status = %q", repo.stageJob.Status)
	}
	var result StageResult
	if err := json.Unmarshal(repo.stageJob.ResultPayload, &result); err != nil {
		t.Fatal(err)
	}
	if result.DiscoveryJobID != testDiscoveryJobID || result.ProvenanceStatus != ProvenanceVerified {
		t.Fatalf("unexpected stage result: %+v", result)
	}
	if len(recorder.events) != 2 || recorder.events[0].Action != "update.stage.requested" || recorder.events[1].Action != "update.stage.completed" {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}

func TestCreateStageReusesRetainedJobWithoutStagingAgain(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.reuseStage = true
	repo.stageJob.Status = StatusSucceeded
	stager := &fakeArtifactStager{}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.createCalls != 1 || repo.markCalls != 0 || stager.stageCalls != 0 {
		t.Fatalf("duplicate stage request performed work: create=%d mark=%d stage=%d", repo.createCalls, repo.markCalls, stager.stageCalls)
	}
}

func TestCreateStageRejectsRetainedCapacityBeforeStaging(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.createErr = ErrStageCapacityExceeded
	stager := &fakeArtifactStager{}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.markCalls != 0 || stager.stageCalls != 0 {
		t.Fatalf("capacity rejection entered staging pipeline: mark=%d stage=%d", repo.markCalls, stager.stageCalls)
	}
}

func TestCreateStageRejectsNonStageableDiscoveryBeforeInsert(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.discoveryJob.Status = StatusFailed
	stager := &fakeArtifactStager{}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.createCalls != 0 || stager.stageCalls != 0 {
		t.Fatalf("non-stageable discovery entered staging pipeline: create=%d stage=%d", repo.createCalls, stager.stageCalls)
	}
}

func TestCreateStageDetachesLifecycleFromRequestCancellation(t *testing.T) {
	repo := newFakeStageRepository(t)
	stager := &fakeArtifactStager{result: successfulStageResult()}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	request := stageRequest(t)
	ctx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(ctx)
	cancel()

	response := httptest.NewRecorder()
	handler.Create(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if stager.ctxErr != nil {
		t.Fatalf("staging inherited cancelled HTTP context: %v", stager.ctxErr)
	}
}

func TestCreateStageFailureCleansAndPersistsSafeCode(t *testing.T) {
	repo := newFakeStageRepository(t)
	stager := &fakeArtifactStager{err: errors.New("remote verifier detail that must not leak")}
	recorder := &fakeAuditRecorder{}
	handler := newStageHandlerWithDependencies(nil, repo, recorder, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.stageJob.Status != StatusFailed || repo.failCode != stageExecutionFailureCode {
		t.Fatalf("failed stage job = %+v, code = %q", repo.stageJob, repo.failCode)
	}
	if len(stager.cleanupCalls) != 1 || stager.cleanupCalls[0] != testJobID {
		t.Fatalf("cleanup calls = %#v", stager.cleanupCalls)
	}
	if strings.Contains(response.Body.String(), "remote verifier") {
		t.Fatalf("raw staging error leaked in response: %s", response.Body.String())
	}
	if len(recorder.events) != 2 || recorder.events[1].Action != "update.stage.failed" || recorder.events[1].Metadata["error_code"] != stageExecutionFailureCode {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}

func TestCreateStageReconcilesCommittedCompletionWithoutDeletingBytes(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.completeErr = context.DeadlineExceeded
	repo.completeCommitThenErr = true
	stager := &fakeArtifactStager{result: successfulStageResult()}
	recorder := &fakeAuditRecorder{}
	handler := newStageHandlerWithDependencies(nil, repo, recorder, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.stageJob.Status != StatusSucceeded || repo.failCode != "" {
		t.Fatalf("reconciled stage job = %+v, failCode=%q", repo.stageJob, repo.failCode)
	}
	if len(stager.cleanupCalls) != 0 {
		t.Fatalf("committed staged bytes were deleted during completion reconciliation: %#v", stager.cleanupCalls)
	}
	if len(recorder.events) != 2 || recorder.events[1].Action != "update.stage.completed" {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}

func TestCreateStageUnknownCompletionStateDoesNotDeleteCandidate(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.completeErr = context.DeadlineExceeded
	repo.stageGetErr = errors.New("database unavailable during reconciliation")
	stager := &fakeArtifactStager{result: successfulStageResult()}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), stageCompletionUncertainCode) {
		t.Fatalf("response does not contain bounded uncertainty code: %s", response.Body.String())
	}
	if len(stager.cleanupCalls) != 0 {
		t.Fatalf("candidate bytes were deleted while completion state was unknown: %#v", stager.cleanupCalls)
	}
	if repo.failCode != "" || repo.stageJob.Status != StatusRunning {
		t.Fatalf("unknown completion state was destructively terminalized: job=%+v code=%q", repo.stageJob, repo.failCode)
	}
}
