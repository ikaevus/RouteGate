package routingprofiles

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRoutingProfileRepository struct {
	profiles []RoutingProfile
	profile  RoutingProfile

	createProfileInput CreateRoutingProfileInput
	updateProfileInput UpdateRoutingProfileInput
	deleteProfileID    string

	createRuleInput CreateRoutingProfileRuleInput
	updateRuleInput UpdateRoutingProfileRuleInput
	deleteRuleID    string
}

func (f *fakeRoutingProfileRepository) ListProfiles(context.Context) ([]RoutingProfile, error) {
	return f.profiles, nil
}

func (f *fakeRoutingProfileRepository) GetProfile(context.Context, string) (RoutingProfile, error) {
	return f.profile, nil
}

func (f *fakeRoutingProfileRepository) CreateProfile(_ context.Context, input CreateRoutingProfileInput) (RoutingProfile, error) {
	f.createProfileInput = input
	return RoutingProfile{ID: "profile-id", Name: input.Name, Description: input.Description, IsDefault: input.IsDefault, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeRoutingProfileRepository) UpdateProfile(_ context.Context, _ string, input UpdateRoutingProfileInput) (RoutingProfile, error) {
	f.updateProfileInput = input
	return RoutingProfile{ID: "profile-id", Name: stringValue(input.Name), Description: stringValue(input.Description), IsDefault: boolValue(input.IsDefault), CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeRoutingProfileRepository) DeleteProfile(_ context.Context, id string) error {
	f.deleteProfileID = id
	return nil
}

func (f *fakeRoutingProfileRepository) CreateRule(_ context.Context, input CreateRoutingProfileRuleInput) (RoutingProfileRule, error) {
	f.createRuleInput = input
	return RoutingProfileRule{ID: "rule-id", RoutingProfileID: input.RoutingProfileID, Name: input.Name, Priority: input.Priority, Action: input.Action, Enabled: input.Enabled, Domains: input.Domains, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeRoutingProfileRepository) UpdateRule(_ context.Context, _ string, ruleID string, input UpdateRoutingProfileRuleInput) (RoutingProfileRule, error) {
	f.updateRuleInput = input
	return RoutingProfileRule{ID: ruleID, Name: stringValue(input.Name), Priority: intValue(input.Priority), Action: stringValue(input.Action), Enabled: boolValue(input.Enabled), Domains: stringSliceValue(input.Domains), CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeRoutingProfileRepository) DeleteRule(_ context.Context, _ string, ruleID string) error {
	f.deleteRuleID = ruleID
	return nil
}

func TestCreateProfileMapsAdminRequest(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles", strings.NewReader(`{
		"name":"  Family split tunnel  ",
		"description":"  Home VPS profile  ",
		"isDefault":true
	}`))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if repository.createProfileInput.Name != "Family split tunnel" {
		t.Fatalf("name = %q, want trimmed profile name", repository.createProfileInput.Name)
	}
	if repository.createProfileInput.Description != "Home VPS profile" || !repository.createProfileInput.IsDefault {
		t.Fatalf("unexpected create input: %+v", repository.createProfileInput)
	}
}

func TestCreateRuleCleansListsAndDefaultsPriority(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles/profile-id/rules", strings.NewReader(`{
		"name":"  YouTube direct  ",
		"action":"direct",
		"domains":[" youtube.com ", "", " youtu.be"],
		"ipCidrs":[" 192.0.2.0/24 "],
		"enabled":true
	}`))
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.CreateRule(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	input := repository.createRuleInput
	if input.RoutingProfileID != "profile-id" || input.Name != "YouTube direct" {
		t.Fatalf("unexpected rule identity: %+v", input)
	}
	if input.Priority != 1000 {
		t.Fatalf("priority = %d, want default 1000", input.Priority)
	}
	if len(input.Domains) != 2 || input.Domains[0] != "youtube.com" || input.Domains[1] != "youtu.be" {
		t.Fatalf("domains were not cleaned: %#v", input.Domains)
	}
	if len(input.IPCIDRs) != 1 || input.IPCIDRs[0] != "192.0.2.0/24" {
		t.Fatalf("ip cidrs were not cleaned: %#v", input.IPCIDRs)
	}
}

func TestCreateRuleRejectsUnknownAction(t *testing.T) {
	repository := &fakeRoutingProfileRepository{}
	handler := testHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/routing-profiles/profile-id/rules", strings.NewReader(`{
		"name":"Bad rule",
		"action":"proxy"
	}`))
	request.SetPathValue("profile_id", "profile-id")
	response := httptest.NewRecorder()

	handler.CreateRule(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "invalid_request" {
		t.Fatalf("unexpected error response: %v", payload)
	}
}

func testHandler(repository routingProfileRepository) *Handler {
	return &Handler{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		profiles: repository,
	}
}
