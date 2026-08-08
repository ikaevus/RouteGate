package configs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeLifecycleRepository struct {
	*fakeApplySafetyRepository
	deleteResult     bool
	deleteErr        error
	currentVersionID string
	activeJob        bool
	pinnedResult     ConfigVersion
}

func (f *fakeLifecycleRepository) DeleteConfigVersion(context.Context, string, string) (bool, error) {
	return f.deleteResult, f.deleteErr
}

func (f *fakeLifecycleRepository) HasActiveConfigApplyJob(context.Context, string, string) (bool, error) {
	return f.activeJob, nil
}

func (f *fakeLifecycleRepository) SetConfigVersionPinned(_ context.Context, _ string, _ string, pinned bool) (ConfigVersion, error) {
	if f.pinnedResult.ID != "" {
		f.pinnedResult.Pinned = pinned
		return f.pinnedResult, nil
	}
	version := f.version
	version.Pinned = pinned
	return version, nil
}

func (f *fakeLifecycleRepository) GetCurrentConfigVersionID(context.Context, string) (string, error) {
	return f.currentVersionID, nil
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

func TestDeleteHistoricalConfigVersion(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id", Status: StatusApplied}},
		deleteResult:              true,
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); err != nil {
		t.Fatalf("delete historical version: %v", err)
	}
}

func TestDeleteConfigVersionRejectsCurrent(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id"}},
		currentVersionID:          "version-id",
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionCurrent) {
		t.Fatalf("expected ErrConfigVersionCurrent, got %v", err)
	}
}

func TestDeleteConfigVersionRejectsPinned(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id", Pinned: true}},
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionPinned) {
		t.Fatalf("expected ErrConfigVersionPinned, got %v", err)
	}
}

func TestDeleteConfigVersionRejectsActiveDeployment(t *testing.T) {
	repo := &fakeLifecycleRepository{
		fakeApplySafetyRepository: &fakeApplySafetyRepository{version: ConfigVersion{ID: "version-id", ServerID: "server-id"}},
		activeJob:                 true,
	}
	service := NewService(repo)
	if err := service.DeleteVersion(context.Background(), "server-id", "version-id"); !errors.Is(err, ErrConfigVersionDeploymentActive) {
		t.Fatalf("expected ErrConfigVersionDeploymentActive, got %v", err)
	}
}

func TestRenderedConfigVersioningIgnoresOnlyRenderedAt(t *testing.T) {
	first := validApplyRenderedConfig(t)
	second := first
	first.Metadata.RenderedAt = time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	second.Metadata.RenderedAt = time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC)

	firstPayload, _ := json.Marshal(first)
	secondPayload, _ := json.Marshal(second)
	equivalent, err := renderedConfigsEquivalentForVersioning(firstPayload, secondPayload)
	if err != nil {
		t.Fatalf("compare rendered configs: %v", err)
	}
	if !equivalent {
		t.Fatal("renderedAt-only change must not create another config version")
	}

	second.Server.Name = "changed-server"
	secondPayload, _ = json.Marshal(second)
	equivalent, err = renderedConfigsEquivalentForVersioning(firstPayload, secondPayload)
	if err != nil {
		t.Fatalf("compare changed rendered configs: %v", err)
	}
	if equivalent {
		t.Fatal("effective config change must create a new version")
	}
}
