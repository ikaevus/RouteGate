package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/artuazh/routegate/backend/internal/agents"
	"github.com/artuazh/routegate/backend/internal/auth"
	"github.com/artuazh/routegate/backend/internal/health"
	"github.com/artuazh/routegate/backend/internal/servers"
)

func NewRouter(logger *slog.Logger) stdhttp.Handler {
	mux := stdhttp.NewServeMux()

	healthHandler := health.NewHandler(logger)
	authHandler := auth.NewHandler(logger)
	serversHandler := servers.NewHandler(logger)
	agentsHandler := agents.NewHandler(logger)

	mux.HandleFunc("GET /api/admin/health", healthHandler.Get)
	mux.HandleFunc("GET /api/agent/health", healthHandler.Get)

	mux.HandleFunc("POST /api/admin/auth/login", authHandler.Login)
	mux.HandleFunc("POST /api/admin/auth/logout", authHandler.Logout)
	mux.HandleFunc("GET /api/admin/me", authHandler.Me)

	mux.HandleFunc("GET /api/admin/servers", serversHandler.List)
	mux.HandleFunc("POST /api/admin/servers", serversHandler.Create)

	mux.HandleFunc("GET /api/admin/agents", agentsHandler.List)
	mux.HandleFunc("POST /api/agent/register", agentsHandler.Register)
	mux.HandleFunc("POST /api/agent/heartbeat", agentsHandler.Heartbeat)

	return loggingMiddleware(logger, mux)
}
