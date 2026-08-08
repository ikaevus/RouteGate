package http

import stdhttp "net/http"

// RouteGate's supported browser surfaces use same-origin /api proxying through
// nginx in production and Vite in development. Do not advertise cross-origin
// Manager access here. OPTIONS is terminated without CORS allow headers, so a
// browser preflight cannot opt an arbitrary external origin into the API.
func corsMiddleware() middleware {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if r.Method == stdhttp.MethodOptions {
				w.WriteHeader(stdhttp.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
