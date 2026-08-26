package agents

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func maxLengthPlatformUpdateVersion(t *testing.T) string {
	t.Helper()
	const prefix = "v1.2.3-"
	emptyPayload, err := platformUpdateTaskPayload("")
	if err != nil {
		t.Fatalf("marshal empty target-version envelope: %v", err)
	}
	maxTargetBytes := maxPlatformUpdateAgentTaskPayloadBytes - len(emptyPayload)
	if maxTargetBytes < len(prefix) {
		t.Fatalf("Agent update-task payload leaves no room for canonical target version")
	}
	return prefix + strings.Repeat("a", maxTargetBytes-len(prefix))
}

func TestPlatformUpdateTargetVersionBoundMatchesAgentTaskLimit(t *testing.T) {
	maxVersion := maxLengthPlatformUpdateVersion(t)
	if !validPlatformUpdateTargetVersion(maxVersion) {
		t.Fatal("largest task-safe canonical target version was rejected")
	}
	payload, err := platformUpdateTaskPayload(maxVersion)
	if err != nil {
		t.Fatalf("marshal largest task-safe payload: %v", err)
	}
	if len(payload) != maxPlatformUpdateAgentTaskPayloadBytes {
		t.Fatalf("largest task-safe payload length=%d want=%d", len(payload), maxPlatformUpdateAgentTaskPayloadBytes)
	}

	tooLong := maxVersion + "a"
	if validPlatformUpdateTargetVersion(tooLong) {
		t.Fatal("target version that exceeds the Agent task payload limit was accepted")
	}
	payload, err = platformUpdateTaskPayload(tooLong)
	if err != nil {
		t.Fatalf("marshal over-limit fixture: %v", err)
	}
	if len(payload) <= maxPlatformUpdateAgentTaskPayloadBytes {
		t.Fatalf("over-limit fixture payload length=%d unexpectedly fits Agent limit", len(payload))
	}
}

func TestCreatePlatformUpdateRejectsVersionThatExceedsAgentTaskLimit(t *testing.T) {
	repository := newPlatformUpdateAwareFakeRepository()
	handler := testAgentHandler(repository)
	tooLong := maxLengthPlatformUpdateVersion(t) + "a"
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/servers/server-id/software-updates",
		strings.NewReader("{\"targetVersion\":\"" + tooLong + "\"}"),
	)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.CreatePlatformUpdate(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_target_version") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.createInput != (CreatePlatformUpdateJobInput{}) {
		t.Fatalf("over-limit version reached repository: %+v", repository.createInput)
	}
}
