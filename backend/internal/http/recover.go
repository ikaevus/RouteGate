package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

func recoverMiddleware(logger *slog.Logger) middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error(
						"panic recovered",
						"panic", recovered,
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", requestIDFromContext(r.Context()),
					)

					httpx.WriteJSON(w, stdhttp.StatusInternalServerError, httpx.Error(
						"internal_server_error",
						"Internal server error.",
					))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
