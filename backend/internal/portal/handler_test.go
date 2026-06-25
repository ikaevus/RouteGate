package portal

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

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

type fakePortalRepository struct {
	profiles []PortalProfile
	profile  PortalProfile
	getErr   error
	listErr  error

	listEmail string
	getEmail  string
	getID     string

	createdInput CreateSubscriptionTokenInput
	createdToken PortalSubscriptionToken
	createErr    error
}

func (f *fakePortalRepository) ListProfilesForUser(_ context.Context, email string) ([]PortalProfile, error) {
	f.listEmail = email
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.profiles, nil
}

func (f *fakePortalRepository) GetProfileForUser(_ context.Context, email string, profileID string) (PortalProfile, error) {
	f.getEmail = email
	f.getID = profileID
	if f.getErr != nil {
		return PortalProfile{}, f.getErr
	}
	if f.profile.ID != "" {
		return f.profile, nil
	}
	return PortalProfile{ID: profileID, DisplayName: "Demo", Status: "active", AccessStatus: AccessStatusActive, Protocol: "sing-box", UpdatedAt: time.Now()}, nil
}

func (f *fakePortalRepository) CreateSubscriptionToken(_ context.Context, input CreateSubscriptionTokenInput) (PortalSubscriptionToken, error) {
	f.createdInput = input
	if f.createErr != nil {
		return PortalSubscriptionToken{}, f.createErr
	}
	if f.createdToken.ID != "" {
		return f.createdToken, nil
	}
	return PortalSubscriptionToken{
		ID:           "token-1",
		VPNAccountID: input.VPNAccountID,
		TokenHash:    input.TokenHash,
		Status:       "active",
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}

func newTestHandler(repo *fakePortalRepository) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		profiles:                  repo,
		generateSubscriptionToken: func() (string, error) { return "fixed-token", nil },
	}
}

func withTestUser(request *http.Request, email string) *http.Request {
	user := auth.AuthenticatedUser{
		UserProfile: auth.UserProfile{
			ID:          "user-1",
			Email:       email,
			Status:      "active",
			Permissions: []string{"portal:access"},
		},
	}
	return request.WithContext(auth.ContextWithUser(request.Context(), user))
}

func TestGenerateSubscriptionAccessReturnsURLAndQRForOwnedActiveProfile(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour).UTC()
	repo := &fakePortalRepository{
		profile: PortalProfile{
			ID:           "account-1",
			DisplayName:  "Alice VPN",
			Status:       "active",
			AccessStatus: AccessStatusActive,
			ExpiresAt:    &expiresAt,
			Protocol:     "sing-box",
			UpdatedAt:    time.Now(),
		},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/portal/profiles/account-1/subscription-token", nil)
	request.SetPathValue("id", "account-1")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "routegate.example")
	request = withTestUser(request, "alice@example.com")
	response := httptest.NewRecorder()

	handler.GenerateSubscriptionAccess(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}
	if repo.getEmail != "alice@example.com" || repo.getID != "account-1" {
		t.Fatalf("expected owner-scoped profile lookup, got email=%q id=%q", repo.getEmail, repo.getID)
	}
	if repo.createdInput.VPNAccountID != "account-1" {
		t.Fatalf("expected account-1 token input, got %q", repo.createdInput.VPNAccountID)
	}
	if repo.createdInput.TokenHash != vpnaccounts.HashSubscriptionToken("fixed-token") {
		t.Fatalf("expected hashed token storage, got %q", repo.createdInput.TokenHash)
	}
	if repo.createdInput.ExpiresAt == nil || !repo.createdInput.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected token expiration to follow profile expiration, got %+v", repo.createdInput.ExpiresAt)
	}

	var body SubscriptionAccessResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	expectedURL := "https://routegate.example/api/v1/subscriptions/fixed-token"
	if !body.Subscription.Available || body.Subscription.SubscriptionURL != expectedURL {
		t.Fatalf("unexpected subscription response: %+v", body.Subscription)
	}
	if !body.QR.Available || body.QR.QRText != expectedURL || body.QR.Format != PortalQRFormat {
		t.Fatalf("unexpected qr response: %+v", body.QR)
	}
}

func TestGenerateSubscriptionAccessRejectsExpiredProfile(t *testing.T) {
	repo := &fakePortalRepository{
		profile: PortalProfile{
			ID:           "account-1",
			DisplayName:  "Expired VPN",
			Status:       "active",
			AccessStatus: AccessStatusExpired,
			Protocol:     "sing-box",
			UpdatedAt:    time.Now(),
		},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/portal/profiles/account-1/subscription-token", nil)
	request.SetPathValue("id", "account-1")
	request = withTestUser(request, "alice@example.com")
	response := httptest.NewRecorder()

	handler.GenerateSubscriptionAccess(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
	}
	if repo.createdInput.VPNAccountID != "" || repo.createdInput.TokenHash != "" {
		t.Fatalf("did not expect token creation for expired profile, got %+v", repo.createdInput)
	}
}

func TestGetProfileHidesForeignProfile(t *testing.T) {
	repo := &fakePortalRepository{getErr: pgx.ErrNoRows}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/portal/profiles/account-2", nil)
	request.SetPathValue("id", "account-2")
	request = withTestUser(request, "alice@example.com")
	response := httptest.NewRecorder()

	handler.GetProfile(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
	if repo.getEmail != "alice@example.com" || repo.getID != "account-2" {
		t.Fatalf("expected owner-scoped profile lookup, got email=%q id=%q", repo.getEmail, repo.getID)
	}
}

func TestSubscriptionAccessResponseDoesNotLeakInternalFields(t *testing.T) {
	repo := &fakePortalRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/portal/profiles/account-1/subscription-token", nil)
	request.SetPathValue("id", "account-1")
	request = withTestUser(request, "alice@example.com")
	response := httptest.NewRecorder()

	handler.GenerateSubscriptionAccess(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}
	body := response.Body.String()
	for _, forbidden := range []string{"tokenHash", "TokenHash", "privateKey", "privateRealityKey"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked forbidden field %q in body %s", forbidden, body)
		}
	}
}
