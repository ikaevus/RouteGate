package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/artuazh/routegate/backend/internal/health"
)

func NewRouter(logger *slog.Logger) stdhttp.Handler {
	mux := stdhttp.NewServeMux()

	healthHandler := health.NewHandler(logger)

	mux.HandleFunc("GET /api/admin/health", healthHandler.Get)
	mux.HandleFunc("GET /api/agent/health", healthHandler.Get)

	return loggingMiddleware(logger, mux)
}
