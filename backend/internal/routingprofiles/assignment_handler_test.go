package routingprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func (f *fakeRoutingProfileRepository) GetServerAssignment(context.Context, string) (ServerRoutingProfileAssignment, error) {
	return f.assignment, f.assignmentErr
}

func (f *fakeRoutingProfileRepository) AssignServerProfile(_ context.Context, input AssignServerRoutingProfileInput) (ServerRoutingProfileAssignment, error) {
	f.assignInput = input
	if f.assignmentErr != nil {
		return ServerRoutingProfileAssignment{}, f.assignmentErr
	}
	now := time.Now()
	return ServerRoutingProfileAssignment{
		ServerID: input.ServerID,
		RoutingProfile: &RoutingProfile{
			ID:        input.RoutingProfileID,
			Name:      "Default split tunnel",
			IsDefault: true,
			CreatedAt: now,
			UpdatedAt: now,
		},
		CreatedAt: &now,
		UpdatedAt: &now,
	}, nil
}

func (f *fakeRoutingProfileRepository) DeleteServerAssignment(_ context.Context, serverID string) error {
	f.deletedServerID = serverID
	return f.assignmentErr
}

func TestAssignServerRoutingProfileMapsRequest(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/servers/server-id/routing-profile", strings.NewReader(`{
		"routingProfileId":"  profile-id  "
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.AssignServerProfile(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if repository.assignInput.ServerID != "server-id" || repository.assignInput.RoutingProfileID != "profile-id" {
		t.Fatalf("unexpected assignment input: %+v", repository.assignInput)
	}
	var payload ServerRoutingProfileAssignment
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.RoutingProfile == nil || payload.RoutingProfile.ID != "profile-id" {
		t.Fatalf("unexpected assignment response: %+v", payload)
	}
}

func TestAssignServerRoutingProfileRejectsMissingProfileID(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/servers/server-id/routing-profile", strings.NewReader(`{
		"routingProfileId":"   "
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.AssignServerProfile(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if repository.assignInput.ServerID != "" {
		t.Fatalf("assignment should not be called: %+v", repository.assignInput)
	}
}

func TestAssignServerRoutingProfileReportsMissingServer(t *testing.T) {
	repository := &fakeRoutingProfileRepository{assignmentErr: ErrServerNotFound}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/servers/missing/routing-profile", strings.NewReader(`{
		"routingProfileId":"profile-id"
	}`))
	request.SetPathValue("server_id", "missing")
	response := httptest.NewRecorder()

	handler.AssignServerProfile(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestAssignServerRoutingProfileReportsMissingProfile(t *testing.T) {
	repository := &fakeRoutingProfileRepository{assignmentErr: pgx.ErrNoRows}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/servers/server-id/routing-profile", strings.NewReader(`{
		"routingProfileId":"missing-profile"
	}`))
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.AssignServerProfile(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "routing_profile_not_found" {
		t.Fatalf("unexpected error response: %v", payload)
	}
}

func TestDeleteServerRoutingProfileAssignmentIsIdempotent(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/server-id/routing-profile", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.DeleteServerAssignment(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if repository.deletedServerID != "server-id" {
		t.Fatalf("deleted server id = %q, want server-id", repository.deletedServerID)
	}
}

func TestDeleteServerRoutingProfileAssignmentReportsMissingServer(t *testing.T) {
	repository := &fakeRoutingProfileRepository{assignmentErr: ErrServerNotFound}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/missing/routing-profile", nil)
	request.SetPathValue("server_id", "missing")
	response := httptest.NewRecorder()

	handler.DeleteServerAssignment(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestGetServerRoutingProfileAssignmentReturnsUnassigned(t *testing.T) {
	repository := &fakeRoutingProfileRepository{assignment: ServerRoutingProfileAssignment{ServerID: "server-id"}}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/routing-profile", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.GetServerAssignment(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"routingProfile":null`) {
		t.Fatalf("expected explicit null routing profile, body=%s", response.Body.String())
	}
}

func TestGetServerRoutingProfileAssignmentPropagatesUnexpectedError(t *testing.T) {
	repository := &fakeRoutingProfileRepository{assignmentErr: errors.New("boom")}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/server-id/routing-profile", nil)
	request.SetPathValue("server_id", "server-id")
	response := httptest.NewRecorder()

	handler.GetServerAssignment(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
}
