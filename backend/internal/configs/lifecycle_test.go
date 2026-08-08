package configs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeLifecycleRepository struct {
	*fakeApplySafetyRepository
	deleteResult bool
	deleteErr    error
}

func (f *fakeLifecycleRepository) DeleteUnusedConfigVersion(context.Context, string, string) (bool, error) {
	return f.deleteResult, f.deleteErr
}

func TestReapplyCreatesNormalApplyJobForPreviouslyAppliedVersion(t *testing.T) {
	rendered := validApplyRenderedConfig(t)
	hash, err := hashRenderedConfig(rendered)
	if err != nil {
		t.Fatalf("hash rendered config: %v", err)
	}
	appliedAt := time.Now().Add(-time.Hour)
	repo := &fakeLifecycleRepository{fakeApplySafetyRepository: &fakeApplySafetyRepository{
		version: ConfigVersion{
			ID:             "version-id",
			ServerID:       "server-id",
			Status:         StatusApplied,
			ConfigHash:     hash,
			RenderedConfig: mustMarshalRaw(t, rendered),
			AppliedAt:      &appliedAt,
		},
		serverInfo: ServerConfigInfo{ID: "server-id", Name: "fi-01", Agent: &AgentConfigInfo{ID: "agent-id"}},
	}}
	service := NewService(repo)

	response, err := service.Reapply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})
	if err != nil {
		t.Fatalf("reapply failed: %v", err)
	}
	if response.Job.ID != "job-id" || repo.createdInput.Action != ApplyJobActionApply {
		t.Fatalf("unexpected redeploy job: response=%+v input=%+v", response, repo.createdInput)
	}
	if repo.createdInput.RequestPayload["redeploy"] != true {
		t.Fatalf("redeploy marker missing: %+v", repo.createdInput.RequestPayload)
	}
}

func TestReapplyRejectsNeverAppliedVersion(t *testing.T) {
	repo := &fakeLifecycleRepository{fakeApplySafetyRepository: &fakeApplySafetyRepository{
		version: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusValidated},
	}}
	service := NewService(repo)

	_, err := service.Reapply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})
	if !errors.Is(err, ErrConfigVersionNeverApplied) {
		t.Fatalf("expected ErrConfigVersionNeverApplied, got %v", err)
	}
}

func TestDeleteUnusedConfigVersion(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id"}},
		deleteResult:              true,
	}
	service := NewService(repo)
	if err := service.DeleteUnused(context.Background(), "server-id", "version-id"); err != nil {
		t.Fatalf("delete unused version: %v", err)
	}
}

func TestDeleteConfigVersionRejectsHistory(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusApplied}},
		deleteResult:              false,
	}
	service := NewService(repo)
	if err := service.DeleteUnused(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionInUse) {
		t.Fatalf("expected ErrConfigVersionInUse, got %v", err)
	}
}
