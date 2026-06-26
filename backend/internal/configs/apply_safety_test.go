package configs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeApplySafetyRepository struct {
	version       ConfigVersion
	versionErr    error
	serverInfo    ServerConfigInfo
	serverInfoErr error
	createdInput  CreateConfigApplyJobInput
	createdJob    ConfigApplyJob
	createJobErr  error
}

func (f *fakeApplySafetyRepository) GetServerConfigInfo(context.Context, string) (ServerConfigInfo, error) {
	return f.serverInfo, f.serverInfoErr
}

func (f *fakeApplySafetyRepository) CreateConfigVersion(context.Context, CreateConfigVersionInput) (ConfigVersion, error) {
	return ConfigVersion{}, nil
}

func (f *fakeApplySafetyRepository) ListConfigVersions(context.Context, string) ([]ConfigVersion, error) {
	return nil, nil
}

func (f *fakeApplySafetyRepository) GetConfigVersion(context.Context, string, string) (ConfigVersion, error) {
	return f.version, f.versionErr
}

func (f *fakeApplySafetyRepository) MarkConfigVersionValidated(context.Context, string, string) (ConfigVersion, error) {
	return ConfigVersion{}, nil
}

func (f *fakeApplySafetyRepository) CreateConfigApplyJob(_ context.Context, input CreateConfigApplyJobInput) (ConfigApplyJob, error) {
	f.createdInput = input
	if f.createJobErr != nil {
		return ConfigApplyJob{}, f.createJobErr
	}
	if f.createdJob.ID != "" {
		return f.createdJob, nil
	}
	return ConfigApplyJob{
		ID:              "job-id",
		ServerID:        input.ServerID,
		AgentID:         input.AgentID,
		ConfigVersionID: input.ConfigVersionID,
		Action:          input.Action,
		Status:          ApplyJobStatusPending,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}, nil
}

func (f *fakeApplySafetyRepository) ListConfigApplyJobs(context.Context, string) ([]ConfigApplyJob, error) {
	return nil, nil
}

func (f *fakeApplySafetyRepository) GetConfigApplyJob(context.Context, string, string) (ConfigApplyJob, error) {
	return ConfigApplyJob{}, nil
}

func TestApplyRejectsTamperedConfigHash(t *testing.T) {
	rendered := validApplyRenderedConfig(t)
	payload := mustMarshalRaw(t, rendered)
	repo := &fakeApplySafetyRepository{
		version: ConfigVersion{
			ID:             "version-id",
			ServerID:       "server-id",
			Status:         StatusValidated,
			ConfigHash:     "tampered-hash",
			RenderedConfig: payload,
		},
		serverInfo: ServerConfigInfo{ID: "server-id", Name: "fi-01", Agent: &AgentConfigInfo{ID: "agent-id"}},
	}
	service := NewService(repo)

	_, err := service.Apply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})

	if !errors.Is(err, ErrConfigHashMismatch) {
		t.Fatalf("expected ErrConfigHashMismatch, got %v", err)
	}
	if repo.createdInput.ConfigVersionID != "" {
		t.Fatalf("apply job must not be created for hash mismatch: %+v", repo.createdInput)
	}
}

func TestApplyRejectsInvalidRenderedConfig(t *testing.T) {
	rendered := validApplyRenderedConfig(t)
	rendered.SingBox.Route.Final = ""
	hash, err := hashRenderedConfig(rendered)
	if err != nil {
		t.Fatalf("hash rendered config: %v", err)
	}
	repo := &fakeApplySafetyRepository{
		version: ConfigVersion{
			ID:             "version-id",
			ServerID:       "server-id",
			Status:         StatusValidated,
			ConfigHash:     hash,
			RenderedConfig: mustMarshalRaw(t, rendered),
		},
		serverInfo: ServerConfigInfo{ID: "server-id", Name: "fi-01", Agent: &AgentConfigInfo{ID: "agent-id"}},
	}
	service := NewService(repo)

	_, err = service.Apply(context.Background(), "server-id", "version-id", ApplyConfigRequest{})

	if !errors.Is(err, ErrConfigApplyUnsafe) {
		t.Fatalf("expected ErrConfigApplyUnsafe, got %v", err)
	}
	if repo.createdInput.ConfigVersionID != "" {
		t.Fatalf("apply job must not be created for invalid rendered config: %+v", repo.createdInput)
	}
}

func TestApplyCreatesJobAfterSafetyChecks(t *testing.T) {
	rendered := validApplyRenderedConfig(t)
	hash, err := hashRenderedConfig(rendered)
	if err != nil {
		t.Fatalf("hash rendered config: %v", err)
	}
	repo := &fakeApplySafetyRepository{
		version: ConfigVersion{
			ID:             "version-id",
			ServerID:       "server-id",
			Status:         StatusValidated,
			ConfigHash:     hash,
			RenderedConfig: mustMarshalRaw(t, rendered),
		},
		serverInfo: ServerConfigInfo{ID: "server-id", Name: "fi-01", Agent: &AgentConfigInfo{ID: "agent-id"}},
	}
	service := NewService(repo)

	response, err := service.Apply(context.Background(), "server-id", "version-id", ApplyConfigRequest{Comment: " deploy now "})

	if err != nil {
		t.Fatalf("expected apply job, got error %v", err)
	}
	if response.Job.ID != "job-id" {
		t.Fatalf("expected job-id, got %+v", response.Job)
	}
	if repo.createdInput.ServerID != "server-id" || repo.createdInput.AgentID != "agent-id" || repo.createdInput.ConfigVersionID != "version-id" {
		t.Fatalf("unexpected apply job input: %+v", repo.createdInput)
	}
	if repo.createdInput.Action != ApplyJobActionApply {
		t.Fatalf("action = %q, want apply", repo.createdInput.Action)
	}
	if repo.createdInput.RequestPayload["comment"] != "deploy now" {
		t.Fatalf("comment payload = %#v, want trimmed comment", repo.createdInput.RequestPayload["comment"])
	}
	if repo.createdInput.RequestPayload["config_hash"] != hash {
		t.Fatalf("config_hash payload = %#v, want %q", repo.createdInput.RequestPayload["config_hash"], hash)
	}
	if _, leaked := repo.createdInput.RequestPayload["rendered_config"]; leaked {
		t.Fatalf("request payload must not include rendered config: %+v", repo.createdInput.RequestPayload)
	}
}

func validApplyRenderedConfig(t *testing.T) RenderedConfig {
	t.Helper()
	return buildRenderedConfig(ServerConfigInfo{
		ID:     "server-id",
		Name:   "fi-01",
		Status: "active",
		Agent: &AgentConfigInfo{
			ID:           "agent-id",
			AgentVersion: "0.1.0",
			Status:       "online",
		},
	}, time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC))
}

func mustMarshalRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw message: %v", err)
	}
	return payload
}
