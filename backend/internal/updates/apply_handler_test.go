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

const testApplyJobID = "44444444-4444-4444-8444-444444444444"

type fakeApplyRepository struct {
	stageJob      Job
	applyJob      Job
	createErr     error
	markErr       error
	completeErr   error
	failCode      string
	createCalls   int
	markCalls     int
	completeCalls int
}

func newFakeApplyRepository(t *testing.T) *fakeApplyRepository {
	t.Helper()
	result := successfulStageResult()
	result.DiscoveryJobID = testDiscoveryJobID
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeApplyRepository{
		stageJob: Job{
			ID:            testJobID,
			Operation:     OperationStage,
			Status:        StatusSucceeded,
			Stage:         StageStage,
			ResultPayload: payload,
		},
		applyJob: Job{
			ID:              testApplyJobID,
			Operation:       OperationApply,
			Status:          StatusPending,
			Stage:           StageApply,
			RequestPayload:  json.RawMessage(`{"stageJobId":"11111111-1111-4111-8111-111111111111"}`),
			ResultPayload:   json.RawMessage(`{}`),
			CreatedByUserID: testUserID,
		},
	}
}

func (r *fakeApplyRepository) CreateApply(context.Context, string, string) (Job, error) {
	r.createCalls++
	if r.createErr != nil {
		return r.applyJob, r.createErr
	}
	return r.applyJob, nil
}

func (r *fakeApplyRepository) MarkRunning(context.Context, string) (Job, error) {
	r.markCalls++
	if r.markErr != nil {
		return Job{}, r.markErr
	}
	r.applyJob.Status = StatusRunning
	return r.applyJob, nil
}

func (r *fakeApplyRepository) CompleteApply(_ context.Context, _ string, result ApplyResult) (Job, error) {
	r.completeCalls++
	if r.completeErr != nil {
		return Job{}, r.completeErr
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	r.applyJob.Status = StatusSucceeded
	r.applyJob.ResultPayload = payload
	return r.applyJob, nil
}

func (r *fakeApplyRepository) Fail(_ context.Context, _ string, errorCode string) (Job, error) {
	r.failCode = errorCode
	r.applyJob.Status = StatusFailed
	r.applyJob.ErrorCode = errorCode
	return r.applyJob, nil
}

func (r *fakeApplyRepository) Get(_ context.Context, id string) (Job, error) {
	switch id {
	case testJobID:
		return r.stageJob, nil
	case testApplyJobID:
		return r.applyJob, nil
	default:
		return Job{}, errors.New("job not found")
	}
}

type fakeApplyDispatcher struct {
	err     error
	calls   int
	stageID string
	ctxErr  error
}

func (d *fakeApplyDispatcher) Apply(ctx context.Context, stageID string) error {
	d.calls++
	d.stageID = stageID
	d.ctxErr = ctx.Err()
	return d.err
}

type fakeStagePinner struct {
	err          error
	pinCalls     int
	releaseCalls int
	stageID      string
}

func (p *fakeStagePinner) Pin(stageID string) (func() error, error) {
	p.pinCalls++
	p.stageID = stageID
	if p.err != nil {
		return nil, p.err
	}
	return func() error {
		p.releaseCalls++
		return nil
	}, nil
}

func applyRequest(t *testing.T) *http.Request {
	t.Helper()
	request := authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/apply")
	body := `{"stageJobId":"` + testJobID + `"}`
	request.Body = io.NopCloser(strings.NewReader(body))
	request.ContentLength = int64(len(body))
	return request
}

func TestCreateApplyCompletesThroughDispatcher(t *testing.T) {
	repo := newFakeApplyRepository(t)
	dispatcher := &fakeApplyDispatcher{}
	pinner := &fakeStagePinner{}
	recorder := &fakeAuditRecorder{}
	handler := newApplyHandlerWithDependencies(nil, repo, recorder, dispatcher)
	handler.pinner = pinner

	response := httptest.NewRecorder()
	handler.Create(response, applyRequest(t))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.createCalls != 1 || repo.markCalls != 1 || repo.completeCalls != 1 || dispatcher.calls != 1 {
		t.Fatalf("unexpected lifecycle calls: create=%d mark=%d complete=%d dispatch=%d", repo.createCalls, repo.markCalls, repo.completeCalls, dispatcher.calls)
	}
	if pinner.pinCalls != 1 || pinner.releaseCalls != 1 || pinner.stageID != testJobID {
		t.Fatalf("unexpected pin lifecycle: pin=%d release=%d stage=%q", pinner.pinCalls, pinner.releaseCalls, pinner.stageID)
	}
	if dispatcher.stageID != testJobID || dispatcher.ctxErr != nil {
		t.Fatalf("unexpected dispatch contract: stage=%q ctxErr=%v", dispatcher.stageID, dispatcher.ctxErr)
	}
	if repo.applyJob.Status != StatusSucceeded {
		t.Fatalf("apply job status = %q", repo.applyJob.Status)
	}
	var result ApplyResult
	if err := json.Unmarshal(repo.applyJob.ResultPayload, &result); err != nil {
		t.Fatal(err)
	}
	if result.StageJobID != testJobID || result.VerifiedCommit != strings.Repeat("a", 40) {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	if len(recorder.events) != 2 || recorder.events[0].Action != "update.apply.requested" || recorder.events[1].Action != "update.apply.completed" {
		t.Fatalf("unexpected audit events: %#v", recorder.events)
	}
}

func TestCreateApplyMarksUnknownOutcomeWithoutRetryAndKeepsPin(t *testing.T) {
	repo := newFakeApplyRepository(t)
	dispatcher := &fakeApplyDispatcher{err: errDispatchOutcomeUnknown}
	pinner := &fakeStagePinner{}
	handler := newApplyHandlerWithDependencies(nil, repo, nil, dispatcher)
	handler.pinner = pinner

	response := httptest.NewRecorder()
	handler.Create(response, applyRequest(t))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if dispatcher.calls != 1 || repo.completeCalls != 0 {
		t.Fatalf("unknown outcome was replayed or completed: dispatch=%d complete=%d", dispatcher.calls, repo.completeCalls)
	}
	if repo.failCode != ErrorCodeApplyOutcomeUnknown {
		t.Fatalf("fail code = %q", repo.failCode)
	}
	if pinner.pinCalls != 1 || pinner.releaseCalls != 0 {
		t.Fatalf("unknown outcome must retain pin: pin=%d release=%d", pinner.pinCalls, pinner.releaseCalls)
	}
}

func TestCreateApplyPinFailureStopsBeforeDispatch(t *testing.T) {
	repo := newFakeApplyRepository(t)
	dispatcher := &fakeApplyDispatcher{}
	pinner := &fakeStagePinner{err: errors.New("pin failed")}
	handler := newApplyHandlerWithDependencies(nil, repo, nil, dispatcher)
	handler.pinner = pinner

	response := httptest.NewRecorder()
	handler.Create(response, applyRequest(t))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if dispatcher.calls != 0 || repo.failCode != applyStagePinFailureCode {
		t.Fatalf("pin failure entered privileged dispatch: calls=%d code=%q", dispatcher.calls, repo.failCode)
	}
}

func TestCreateApplyRejectsNonCanonicalStageBeforeJobInsert(t *testing.T) {
	repo := newFakeApplyRepository(t)
	repo.stageJob.ResultPayload = json.RawMessage(`{"retention":"evicted"}`)
	dispatcher := &fakeApplyDispatcher{}
	handler := newApplyHandlerWithDependencies(nil, repo, nil, dispatcher)

	response := httptest.NewRecorder()
	handler.Create(response, applyRequest(t))

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.createCalls != 0 || dispatcher.calls != 0 {
		t.Fatalf("invalid stage entered apply pipeline: create=%d dispatch=%d", repo.createCalls, dispatcher.calls)
	}
}

func TestDecodeApplyRequestRejectsUnknownFieldsAndNonV4UUID(t *testing.T) {
	for _, body := range []string{
		`{"stageJobId":"11111111-1111-1111-8111-111111111111"}`,
		`{"stageJobId":"11111111-1111-4111-8111-111111111111","role":"vpn"}`,
		`{"stageJobId":"/tmp/candidate"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		if _, err := decodeApplyRequest(request); err == nil {
			t.Fatalf("request unexpectedly accepted: %s", body)
		}
	}
}
