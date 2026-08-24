package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type cancellationAwareRepository struct {
	job    Job
	cancel context.CancelFunc
}

func newCancellationAwareRepository(cancel context.CancelFunc) *cancellationAwareRepository {
	return &cancellationAwareRepository{
		cancel: cancel,
		job: Job{
			ID:             testJobID,
			Operation:      OperationPreflight,
			Status:         StatusPending,
			Stage:          StagePreflight,
			RequestPayload: json.RawMessage(`{}`),
			ResultPayload:  json.RawMessage(`{}`),
		},
	}
}

func (r *cancellationAwareRepository) CreatePreflight(ctx context.Context, _ string) (Job, error) {
	r.cancel()
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	return r.job, nil
}

func (r *cancellationAwareRepository) MarkRunning(ctx context.Context, _ string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	r.job.Status = StatusRunning
	return r.job, nil
}

func (r *cancellationAwareRepository) CompletePreflight(ctx context.Context, _ string, result PreflightResult) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return Job{}, err
	}
	r.job.Status = StatusSucceeded
	r.job.ResultPayload = payload
	return r.job, nil
}

func (r *cancellationAwareRepository) Fail(ctx context.Context, _ string, errorCode string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	r.job.Status = StatusFailed
	r.job.ErrorCode = errorCode
	return r.job, nil
}

func (r *cancellationAwareRepository) Get(context.Context, string) (Job, error) {
	return r.job, nil
}

func (r *cancellationAwareRepository) List(context.Context, int) ([]Job, error) {
	return []Job{r.job}, nil
}

type cancellationAwareSchemaReader struct{}

func (cancellationAwareSchemaReader) AppliedSchemaVersion(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "000136_update_job_discovery", nil
}

func TestCreatePreflightSurvivesRequestCancellationDuringInsert(t *testing.T) {
	request := authenticatedRequest(http.MethodPost, "/api/v1/system/update-jobs/preflight")
	requestCtx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(requestCtx)

	repo := newCancellationAwareRepository(cancel)
	handler := NewHandlerWithDependencies(nil, repo, cancellationAwareSchemaReader{}, nil, releaseInfo)
	response := httptest.NewRecorder()

	handler.CreatePreflight(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.job.Status != StatusSucceeded {
		t.Fatalf("job status = %q, want succeeded", repo.job.Status)
	}
	var result PreflightResult
	if err := json.Unmarshal(repo.job.ResultPayload, &result); err != nil {
		t.Fatalf("decode preflight result: %v", err)
	}
	if result.Decision != DecisionProceed {
		t.Fatalf("decision = %q, want proceed", result.Decision)
	}
}
