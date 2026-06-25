package portal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/auth"
)

func withPortalTestUser(request *http.Request, email string, status string, permissions []string) *http.Request {
	user := auth.AuthenticatedUser{
		UserProfile: auth.UserProfile{
			ID:          "user-1",
			Email:       email,
			Status:      status,
			Permissions: permissions,
		},
	}
	return request.WithContext(auth.ContextWithUser(request.Context(), user))
}

func TestPortalAccessRequiresPortalPermission(t *testing.T) {
	repo := &fakePortalRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/portal/profiles/account-1", nil)
	request.SetPathValue("id", "account-1")
	request = withPortalTestUser(request, "alice@example.com", "active", []string{"vpn_users:read"})
	response := httptest.NewRecorder()

	handler.GetProfile(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
	}
	if repo.getEmail != "" || repo.getID != "" {
		t.Fatalf("did not expect profile lookup without portal permission, got email=%q id=%q", repo.getEmail, repo.getID)
	}
}

func TestGenerateSubscriptionAccessRejectsInactiveUser(t *testing.T) {
	repo := &fakePortalRepository{
		profile: PortalProfile{
			ID:           "account-1",
			DisplayName:  "Alice VPN",
			Status:       "active",
			AccessStatus: AccessStatusActive,
			Protocol:     "sing-box",
			UpdatedAt:    time.Now(),
		},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/portal/profiles/account-1/subscription-token", nil)
	request.SetPathValue("id", "account-1")
	request = withPortalTestUser(request, "alice@example.com", "disabled", []string{"portal:access"})
	response := httptest.NewRecorder()

	handler.GenerateSubscriptionAccess(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
	}
	if repo.getEmail != "" || repo.getID != "" {
		t.Fatalf("did not expect profile lookup for inactive user, got email=%q id=%q", repo.getEmail, repo.getID)
	}
	if repo.createdInput.VPNAccountID != "" || repo.createdInput.TokenHash != "" {
		t.Fatalf("did not expect token creation for inactive user, got %+v", repo.createdInput)
	}
}

func TestGenerateSubscriptionAccessRejectsSuspendedProfile(t *testing.T) {
	repo := &fakePortalRepository{
		profile: PortalProfile{
			ID:           "account-1",
			DisplayName:  "Suspended VPN",
			Status:       "suspended",
			AccessStatus: AccessStatusSuspended,
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
		t.Fatalf("did not expect token creation for suspended profile, got %+v", repo.createdInput)
	}
}

func TestPortalSubscriptionAndQRMetadataHideForeignProfile(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		handle  func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "subscription metadata",
			method: http.MethodGet,
			path:   "/api/portal/profiles/account-foreign/subscription",
			handle: newTestHandler(&fakePortalRepository{getErr: pgx.ErrNoRows}).GetSubscription,
		},
		{
			name:   "qr metadata",
			method: http.MethodGet,
			path:   "/api/portal/profiles/account-foreign/qr",
			handle: newTestHandler(&fakePortalRepository{getErr: pgx.ErrNoRows}).GetQRCode,
		},
		{
			name:   "subscription token generation",
			method: http.MethodPost,
			path:   "/api/portal/profiles/account-foreign/subscription-token",
			handle: newTestHandler(&fakePortalRepository{getErr: pgx.ErrNoRows}).GenerateSubscriptionAccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.SetPathValue("id", "account-foreign")
			request = withTestUser(request, "alice@example.com")
			response := httptest.NewRecorder()

			tt.handle(response, request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
			}
		})
	}
}

func TestSubscriptionAndQRMetadataForInactiveProfilesIsUnavailable(t *testing.T) {
	expiredAt := time.Now().Add(-time.Hour).UTC()
	profiles := []PortalProfile{
		{
			ID:           "account-suspended",
			DisplayName:  "Suspended VPN",
			Status:       "suspended",
			AccessStatus: AccessStatusSuspended,
			Protocol:     "sing-box",
			UpdatedAt:    time.Now(),
		},
		{
			ID:           "account-expired",
			DisplayName:  "Expired VPN",
			Status:       "active",
			AccessStatus: AccessStatusExpired,
			ExpiresAt:    &expiredAt,
			Protocol:     "sing-box",
			UpdatedAt:    time.Now(),
		},
	}

	for _, profile := range profiles {
		t.Run(profile.AccessStatus+" subscription", func(t *testing.T) {
			repo := &fakePortalRepository{profile: profile}
			handler := newTestHandler(repo)
			request := httptest.NewRequest(http.MethodGet, "/api/portal/profiles/"+profile.ID+"/subscription", nil)
			request.SetPathValue("id", profile.ID)
			request = withTestUser(request, "alice@example.com")
			response := httptest.NewRecorder()

			handler.GetSubscription(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
			}
			var body SubscriptionResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Subscription.Available || body.Subscription.SubscriptionURL != "" || body.Subscription.RequiresTokenRotation {
				t.Fatalf("expected unavailable subscription metadata, got %+v", body.Subscription)
			}
			if body.Subscription.AccessStatus != profile.AccessStatus {
				t.Fatalf("expected access status %q, got %q", profile.AccessStatus, body.Subscription.AccessStatus)
			}
		})

		t.Run(profile.AccessStatus+" qr", func(t *testing.T) {
			repo := &fakePortalRepository{profile: profile}
			handler := newTestHandler(repo)
			request := httptest.NewRequest(http.MethodGet, "/api/portal/profiles/"+profile.ID+"/qr", nil)
			request.SetPathValue("id", profile.ID)
			request = withTestUser(request, "alice@example.com")
			response := httptest.NewRecorder()

			handler.GetQRCode(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
			}
			var body QRCodeResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.QR.Available || body.QR.QRText != "" {
				t.Fatalf("expected unavailable qr metadata, got %+v", body.QR)
			}
			if body.QR.AccessStatus != profile.AccessStatus {
				t.Fatalf("expected access status %q, got %q", profile.AccessStatus, body.QR.AccessStatus)
			}
		})
	}
}

func TestPortalResponsesDoNotLeakServerSideSecrets(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour).UTC()
	profile := PortalProfile{
		ID:           "account-1",
		DisplayName:  "Alice VPN",
		Status:       "active",
		AccessStatus: AccessStatusActive,
		ExpiresAt:    &expiresAt,
		Protocol:     "sing-box",
		Location:     "FI",
		UpdatedAt:    time.Now(),
	}

	tests := []struct {
		name    string
		method  string
		path    string
		handle  func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{
			name:   "profiles list",
			method: http.MethodGet,
			path:   "/api/portal/profiles",
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.ListProfiles(w, r) },
		},
		{
			name:   "profile detail",
			method: http.MethodGet,
			path:   "/api/portal/profiles/account-1",
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.GetProfile(w, r) },
		},
		{
			name:   "subscription metadata",
			method: http.MethodGet,
			path:   "/api/portal/profiles/account-1/subscription",
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.GetSubscription(w, r) },
		},
		{
			name:   "qr metadata",
			method: http.MethodGet,
			path:   "/api/portal/profiles/account-1/qr",
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.GetQRCode(w, r) },
		},
		{
			name:   "subscription access generation",
			method: http.MethodPost,
			path:   "/api/portal/profiles/account-1/subscription-token",
			handle: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.GenerateSubscriptionAccess(w, r) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakePortalRepository{profiles: []PortalProfile{profile}, profile: profile}
			handler := newTestHandler(repo)
			request := httptest.NewRequest(tt.method, tt.path, nil)
			request.SetPathValue("id", profile.ID)
			request = withTestUser(request, "alice@example.com")
			response := httptest.NewRecorder()

			tt.handle(handler, response, request)

			if response.Code < http.StatusOK || response.Code >= http.StatusMultipleChoices {
				t.Fatalf("expected success status, got %d with body %s", response.Code, response.Body.String())
			}
			assertNoPortalSecretLeaks(t, response.Body.String())
		})
	}
}

func assertNoPortalSecretLeaks(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{
		"tokenHash",
		"TokenHash",
		"token_hash",
		"privateRealityKey",
		"realityPrivateKey",
		"reality_private_key",
		"x25519PrivateKey",
		"privateKey",
		"private_key",
		"registrationToken",
		"registration_token",
		"serverSecret",
		"server_secret",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("portal response leaked forbidden field %q in body %s", forbidden, body)
		}
	}
}
