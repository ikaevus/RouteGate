package vpnaccounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateSubscriptionTokenRejectsExpiredExpiresAt(t *testing.T) {
	repo := &fakeAccountRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/subscription-token", strings.NewReader("{\"expiresAt\":\"2000-01-01T00:00:00Z\"}"))
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.CreateSubscriptionToken(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	if repo.createdTokenInput.VPNAccountID != "" {
		t.Fatalf("expected no subscription token creation, got input %+v", repo.createdTokenInput)
	}
}

func TestCreateSubscriptionTokenFallsBackFromInvalidForwardedHeaders(t *testing.T) {
	repo := &fakeAccountRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/subscription-token", strings.NewReader(`{}`))
	request.SetPathValue("id", "account-1")
	request.Host = "manager.routegate.local"
	request.Header.Set("X-Forwarded-Proto", "javascript")
	request.Header.Set("X-Forwarded-Host", "evil.example/path")
	response := httptest.NewRecorder()

	handler.CreateSubscriptionToken(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}

	var body SubscriptionTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "http://manager.routegate.local/api/v1/subscriptions/fixed-token"
	if body.SubscriptionURL != want {
		t.Fatalf("expected subscription URL %q, got %q", want, body.SubscriptionURL)
	}
}

func TestGetPublicSubscriptionRejectsExpiredToken(t *testing.T) {
	expiresAt := time.Now().Add(-time.Minute)
	repo := &fakeAccountRepository{
		findToken: SubscriptionToken{
			ID:           "token-1",
			VPNAccountID: "account-1",
			Status:       SubscriptionTokenStatusActive,
			ExpiresAt:    &expiresAt,
		},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/fixed-token", nil)
	request.SetPathValue("token", "fixed-token")
	response := httptest.NewRecorder()

	handler.GetPublicSubscription(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
	if repo.usedTokenID != "" {
		t.Fatalf("expected expired token not marked used, got %q", repo.usedTokenID)
	}
}
