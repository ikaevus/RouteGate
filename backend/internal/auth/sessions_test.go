package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsCurrentSession(t *testing.T) {
	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{name: "same session", current: "session-a", target: "session-a", want: true},
		{name: "trims target", current: "session-a", target: " session-a ", want: true},
		{name: "different session", current: "session-a", target: "session-b", want: false},
		{name: "empty current", current: "", target: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCurrentSession(tt.current, tt.target); got != tt.want {
				t.Fatalf("isCurrentSession(%q, %q) = %v, want %v", tt.current, tt.target, got, tt.want)
			}
		})
	}
}

func TestRevokeOtherSessionRejectsCurrentSession(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/sessions/current-session", nil)
	request.SetPathValue("session_id", "current-session")
	request = request.WithContext(ContextWithUser(request.Context(), AuthenticatedUser{
		UserProfile: UserProfile{ID: "user-1"},
		SessionID:   "current-session",
	}))
	response := httptest.NewRecorder()

	handler.RevokeOtherSession(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if !strings.Contains(response.Body.String(), "current_session") {
		t.Fatalf("response body %q does not contain current_session", response.Body.String())
	}
}

func TestListSessionsRequiresAuthentication(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	response := httptest.NewRecorder()

	handler.ListSessions(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
