package http

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/config"
)

func TestPlatformUpdateRolloutRoutesRequireExactPermissions(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		permission string
	}{
		{name: "create", method: stdhttp.MethodPost, path: "/api/v1/platform-update-rollouts", permission: "system:manage"},
		{name: "get", method: stdhttp.MethodGet, path: "/api/v1/platform-update-rollouts/550e8400-e29b-41d4-a716-446655440000", permission: "servers:read"},
		{name: "advance", method: stdhttp.MethodPost, path: "/api/v1/platform-update-rollouts/550e8400-e29b-41d4-a716-446655440000/advance", permission: "system:manage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			endpoint := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				calls++
				w.WriteHeader(stdhttp.StatusNoContent)
			})
			mux := stdhttp.NewServeMux()
			registerPlatformUpdateRolloutRoutes(mux, func(next stdhttp.Handler) stdhttp.Handler { return next }, endpoint, endpoint, endpoint)

			request := httptest.NewRequest(test.method, test.path, nil)
			request = request.WithContext(auth.ContextWithUser(context.Background(), auth.AuthenticatedUser{UserProfile: auth.UserProfile{
				Permissions: []string{test.permission},
			}}))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != stdhttp.StatusNoContent || calls != 1 {
				t.Fatalf("allowed request status=%d calls=%d", response.Code, calls)
			}

			request = httptest.NewRequest(test.method, test.path, nil)
			request = request.WithContext(auth.ContextWithUser(context.Background(), auth.AuthenticatedUser{UserProfile: auth.UserProfile{
				Permissions: []string{"unrelated:permission"},
			}}))
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != stdhttp.StatusForbidden || calls != 1 {
				t.Fatalf("denied request status=%d calls=%d", response.Code, calls)
			}
		})
	}
}

func TestPlatformUpdateRolloutRoutesUseCoreMiddlewareChain(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewRouter(config.Config{}, logger, nil)

	unauthenticated := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/platform-update-rollouts", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthenticated)
	if response.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want %d", response.Code, stdhttp.StatusUnauthorized)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("rollout route bypassed request-id middleware")
	}

	preflight := httptest.NewRequest(stdhttp.MethodOptions, "/api/v1/platform-update-rollouts", nil)
	preflight.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Code != stdhttp.StatusNoContent {
		t.Fatalf("preflight status=%d, want %d", response.Code, stdhttp.StatusNoContent)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("rollout route unexpectedly enabled cross-origin access")
	}
}
