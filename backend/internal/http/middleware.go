package http

import (
	"log/slog"
	stdhttp "net/http"
	"time"
)

type middleware func(stdhttp.Handler) stdhttp.Handler

func chain(handler stdhttp.Handler, middlewares ...middleware) stdhttp.Handler {
	for index := len(middlewares) - 1; index >= 0; index-- {
		handler = middlewares[index](handler)
	}

	return handler
}

func loggingMiddleware(logger *slog.Logger) middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			startedAt := time.Now()

			next.ServeHTTP(w, r)

			logger.Info(
				"HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
				"request_id", requestIDFromContext(r.Context()),
				"duration_ms", time.Since(startedAt).Milliseconds(),
			)
		})
	}
}
