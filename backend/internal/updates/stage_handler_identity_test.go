package updates

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateStageMarkRunningFailureKeepsCreatedJobIdentity(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.markErr = errors.New("mark running failed")
	stager := &fakeArtifactStager{}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.failCode != stageStateTransitionCode {
		t.Fatalf("fail code = %q, want %q", repo.failCode, stageStateTransitionCode)
	}
	if len(stager.cleanupCalls) != 1 || stager.cleanupCalls[0] != testJobID {
		t.Fatalf("cleanup lost durable stage job identity: %#v", stager.cleanupCalls)
	}
}

func TestCreateStageUncommittedCompletionFailureCleansCreatedJobIdentity(t *testing.T) {
	repo := newFakeStageRepository(t)
	repo.completeErr = errors.New("completion rejected")
	stager := &fakeArtifactStager{result: successfulStageResult()}
	handler := newStageHandlerWithDependencies(nil, repo, nil, stager)

	response := httptest.NewRecorder()
	handler.Create(response, stageRequest(t))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if repo.failCode != stageCompletionFailureCode {
		t.Fatalf("fail code = %q, want %q", repo.failCode, stageCompletionFailureCode)
	}
	if len(stager.cleanupCalls) != 1 || stager.cleanupCalls[0] != testJobID {
		t.Fatalf("completion failure cleanup lost durable stage job identity: %#v", stager.cleanupCalls)
	}
}
