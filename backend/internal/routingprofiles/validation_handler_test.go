package routingprofiles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingRoutingProfileRepository struct {
	*fakeRoutingProfileRepository
	createProfileErr error
	deleteProfileErr error
}

func (f *failingRoutingProfileRepository) CreateProfile(ctx context.Context, input CreateRoutingProfileInput) (RoutingProfile, error) {
	if f.createProfileErr != nil {
		return RoutingProfile{}, f.createProfileErr
	}
	return f.fakeRoutingProfileRepository.CreateProfile(ctx, input)
}

func (f *failingRoutingProfileRepository) DeleteProfile(ctx context.Context, id string) error {
	if f.deleteProfileErr != nil {
		return f.deleteProfileErr
	}
	return f.fakeRoutingProfileRepository.DeleteProfile(ctx, id)
}

func TestCreateProfileRejectsBlankName(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles", strings.NewReader(`{"name":"   "}`))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if repository.createProfileInput.Name != "" {
		t.Fatalf("create should not be called: %+v", repository.createProfileInput)
	}
}

func TestCreateProfileReportsDuplicateName(t *testing.T) {
	repository := &failingRoutingProfileRepository{
		fakeRoutingProfileRepository: &fakeRoutingProfileRepository{},
		createProfileErr:            ErrRoutingProfileNameAlreadyExists,
	}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles", strings.NewReader(`{"name":"Default direct"}`))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	assertRoutingProfileErrorStatus(t, response, "routing_profile_name_exists")
}

func TestUpdateProfileRejectsTooLongName(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/routing-profiles/profile-id", strings.NewReader(`{"name":"`+strings.Repeat("a", 121)+`"}`))
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.Update(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestDeleteProfileReportsAssignedConflict(t *testing.T) {
	repository := &failingRoutingProfileRepository{
		fakeRoutingProfileRepository: &fakeRoutingProfileRepository{},
		deleteProfileErr:            ErrRoutingProfileAssigned,
	}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/routing-profiles/profile-id", nil)
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.Delete(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	assertRoutingProfileErrorStatus(t, response, "routing_profile_assigned")
}

func TestCreateRuleRejectsMissingMatchers(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles/profile-id/rules", strings.NewReader(`{
		"name":"No matcher",
		"action":"direct"
	}`))
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.CreateRule(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	assertRoutingProfileErrorStatus(t, response, "invalid_request")
}

func TestCreateRuleRejectsInvalidCIDR(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles/profile-id/rules", strings.NewReader(`{
		"name":"Bad CIDR",
		"action":"block",
		"ipCidrs":["192.0.2.999/24"]
	}`))
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.CreateRule(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	assertRoutingProfileErrorStatus(t, response, "invalid_request")
}

func TestCreateRuleRejectsInvalidDomain(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles/profile-id/rules", strings.NewReader(`{
		"name":"Bad domain",
		"action":"direct",
		"domains":["https://example.com/path"]
	}`))
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.CreateRule(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	assertRoutingProfileErrorStatus(t, response, "invalid_request")
}

func TestUpdateRuleRejectsInvalidCIDR(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/routing-profiles/profile-id/rules/rule-id", strings.NewReader(`{
		"ipCidrs":["not-a-cidr"]
	}`))
	request.SetPathValue("profile_id", "profile-id")
	request.SetPathValue("rule_id", "rule-id")
	response := httptest.NewRecorder()

	handler.UpdateRule(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func assertRoutingProfileErrorStatus(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != want {
		t.Fatalf("unexpected error response: %v", payload)
	}
}
