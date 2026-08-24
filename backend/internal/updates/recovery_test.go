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

func (r fakeInterruptedJobRepository) RecoverInterruptedJobs(context.Context) ([]Job, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.jobs, nil
}

type fakeInterruptedStageCleaner struct {
	ids []string
	err error
}

func (c *fakeInterruptedStageCleaner) Cleanup(id string) error {
	c.ids = append(c.ids, id)
	return c.err
}

func TestRecoverInterruptedJobsAuditsPreflightAndDiscovery(t *testing.T) {
	recorder := &fakeAuditRecorder{}
	discoveryJobID := "33333333-3333-4333-8333-333333333333"
	repo := fakeInterruptedJobRepository{jobs: []Job{
		{ID: testJobID, Operation: OperationPreflight, Stage: StagePreflight, Status: StatusFailed, ErrorCode: ErrorCodePreflightInterrupted},
		{ID: discoveryJobID, Operation: OperationDiscovery, Stage: StageDiscovery, Status: StatusFailed, ErrorCode: ErrorCodeDiscoveryInterrupted},
	}}

	if err := recoverInterruptedJobs(context.Background(), nil, repo, recorder); err != nil {
		t.Fatalf("recover interrupted jobs: %v", err)
	}
	if len(recorder.events) != 2 {
		t.Fatalf("audit events = %d, want 2", len(recorder.events))
	}

	preflight := recorder.events[0]
	if preflight.ActorType != audit.ActorTypeSystem || preflight.Action != "update.preflight.interrupted" || preflight.Result != audit.ResultFailure {
		t.Fatalf("unexpected preflight audit event: %+v", preflight)
	}
	if preflight.ResourceType != "update_job" || preflight.ResourceID != testJobID || preflight.Metadata["error_code"] != ErrorCodePreflightInterrupted {
		t.Fatalf("unexpected preflight audit metadata: %+v", preflight)
	}

	discovery := recorder.events[1]
	if discovery.ActorType != audit.ActorTypeSystem || discovery.Action != "update.discovery.interrupted" || discovery.Result != audit.ResultFailure {
		t.Fatalf("unexpected discovery audit event: %+v", discovery)
	}
	if discovery.ResourceType != "update_job" || discovery.ResourceID != discoveryJobID || discovery.Metadata["error_code"] != ErrorCodeDiscoveryInterrupted {
		t.Fatalf("unexpected discovery audit metadata: %+v", discovery)
	}
}

func TestRecoverInterruptedStageCleansStagingStateBeforeAudit(t *testing.T) {
	stageJobID := "44444444-4444-4444-8444-444444444444"
	repo := fakeInterruptedJobRepository{jobs: []Job{
		{ID: stageJobID, Operation: OperationStage, Stage: StageStage, Status: StatusFailed, ErrorCode: ErrorCodeStageInterrupted},
	}}
	recorder := &fakeAuditRecorder{}
	cleaner := &fakeInterruptedStageCleaner{}

	if err := recoverInterruptedJobsWithCleaner(context.Background(), nil, repo, recorder, cleaner); err != nil {
		t.Fatalf("recover interrupted stage: %v", err)
	}
	if len(cleaner.ids) != 1 || cleaner.ids[0] != stageJobID {
		t.Fatalf("cleaned stage IDs = %#v, want %s", cleaner.ids, stageJobID)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.ActorType != audit.ActorTypeSystem || event.Action != "update.stage.interrupted" || event.Result != audit.ResultFailure {
		t.Fatalf("unexpected stage recovery audit event: %+v", event)
	}
	if event.ResourceID != stageJobID || event.Metadata["error_code"] != ErrorCodeStageInterrupted {
		t.Fatalf("unexpected stage recovery metadata: %+v", event)
	}
}

func TestRecoverInterruptedStageFailsClosedWhenCleanupFails(t *testing.T) {
	stageJobID := "55555555-5555-4555-8555-555555555555"
	want := errors.New("cleanup denied")
	repo := fakeInterruptedJobRepository{jobs: []Job{
		{ID: stageJobID, Operation: OperationStage, Stage: StageStage, Status: StatusFailed, ErrorCode: ErrorCodeStageInterrupted},
	}}
	recorder := &fakeAuditRecorder{}
	cleaner := &fakeInterruptedStageCleaner{err: want}

	err := recoverInterruptedJobsWithCleaner(context.Background(), nil, repo, recorder, cleaner)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(recorder.events) != 0 {
		t.Fatalf("audit should not report completed recovery after cleanup failure: %#v", recorder.events)
	}
}

func TestRecoverInterruptedJobsPropagatesRepositoryFailure(t *testing.T) {
	want := errors.New("recovery unavailable")
	err := recoverInterruptedJobs(context.Background(), nil, fakeInterruptedJobRepository{err: want}, &fakeAuditRecorder{})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
