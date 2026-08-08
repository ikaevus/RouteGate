package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddlewareDoesNotReflectCrossOrigin(t *testing.T) {
	called := false
	handler := corsMiddleware()(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		called = true
		w.WriteHeader(stdhttp.StatusOK)
	}))

	request := httptest.NewRequest(stdhttp.MethodGet, "https://routegate.example.com/api/admin/health", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called {
		t.Fatal("same-origin application request did not reach the next handler")
	}
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusOK)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty", got)
	}
}

func TestCORSMiddlewarePreflightDoesNotEnableCrossOrigin(t *testing.T) {
	called := false
	handler := corsMiddleware()(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		called = true
		w.WriteHeader(stdhttp.StatusOK)
	}))

	request := httptest.NewRequest(stdhttp.MethodOptions, "https://routegate.example.com/api/v1/auth/login", nil)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Access-Control-Request-Method", stdhttp.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if called {
		t.Fatal("preflight request unexpectedly reached the application handler")
	}
	if response.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, stdhttp.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want empty", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want empty", got)
	}
}
