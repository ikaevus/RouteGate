package updates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type cleanupFailingArtifactStager struct {
	cleanupIDs []string
}

func (s *cleanupFailingArtifactStager) StageAndVerify(context.Context, string, DiscoveryResult) (StageResult, error) {
	return StageResult{}, errors.New("staging failed")
}

func (s *cleanupFailingArtifactStager) Cleanup(jobID string) error {
	s.cleanupIDs = append(s.cleanupIDs, jobID)
	return errors.New("cleanup failed")
}

func TestCreateStageCleanupFailureLeavesJobRetryable(t *testing.T) {
	repo := newFakeStageRepository(t)
	stager := &cleanupFailingArtifactStager{}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(stager.cleanupIDs) != 1 || stager.cleanupIDs[0] != testJobID {
		t.Fatalf("cleanup IDs = %#v, want %s", stager.cleanupIDs, testJobID)
	}
	if repo.failCode != "" {
		t.Fatalf("stage job was terminalized after cleanup failure: %q", repo.failCode)
	}
	if repo.stageJob.Status != StatusRunning {
		t.Fatalf("stage job status = %q, want retryable running", repo.stageJob.Status)
	}
}
