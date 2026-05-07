package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/artuazh/routegate/backend/internal/auth"
	"github.com/artuazh/routegate/backend/internal/health"
)

func NewRouter(logger *slog.Logger) stdhttp.Handler {
	mux := stdhttp.NewServeMux()

	healthHandler := health.NewHandler(logger)
	authHandler := auth.NewHandler(logger)

	mux.HandleFunc("GET /api/admin/health", healthHandler.Get)
	mux.HandleFunc("GET /api/agent/health", healthHandler.Get)

	mux.HandleFunc("POST /api/admin/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/admin/auth/logout", authHandler.Logout)
	mux.HandleFunc("GET /api/admin/me", authHandler.Me)

	return loggingMiddleware(logger, mux)
}
