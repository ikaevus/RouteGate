package updates

import (
	"context"
	"errors"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/audit"
)

type fakeInterruptedJobRepository struct {
	jobs []Job
	err  error
}

func (r fakeInterruptedJobRepository) RecoverInterruptedPreflights(context.Context) ([]Job, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.jobs, nil
}

func TestRecoverInterruptedJobsAuditsRecoveredPreflights(t *testing.T) {
	recorder := &fakeAuditRecorder{}
	repo := fakeInterruptedJobRepository{jobs: []Job{
		{ID: testJobID, Operation: OperationPreflight, Stage: StagePreflight, Status: StatusFailed, ErrorCode: ErrorCodePreflightInterrupted},
	}}

	if err := recoverInterruptedJobs(context.Background(), nil, repo, recorder); err != nil {
		t.Fatalf("recover interrupted jobs: %v", err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.ActorType != audit.ActorTypeSystem || event.Action != "update.preflight.interrupted" || event.Result != audit.ResultFailure {
		t.Fatalf("unexpected audit event: %+v", event)
	}
	if event.ResourceType != "update_job" || event.ResourceID != testJobID {
		t.Fatalf("unexpected audit resource: %+v", event)
	}
	if event.Metadata["error_code"] != ErrorCodePreflightInterrupted || event.Metadata["status"] != StatusFailed {
		t.Fatalf("unexpected audit metadata: %#v", event.Metadata)
	}
}

func TestRecoverInterruptedJobsPropagatesRepositoryFailure(t *testing.T) {
	want := errors.New("recovery unavailable")
	err := recoverInterruptedJobs(context.Background(), nil, fakeInterruptedJobRepository{err: want}, &fakeAuditRecorder{})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
