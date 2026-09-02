package http

import (
	stdhttp "net/http"

	"github.com/ikaevus/routegate/backend/internal/auth"
)

func registerPlatformUpdateRolloutRoutes(
	mux *stdhttp.ServeMux,
	authn func(stdhttp.Handler) stdhttp.Handler,
	create stdhttp.Handler,
	get stdhttp.Handler,
	advance stdhttp.Handler,
) {
	mux.Handle("POST /api/v1/platform-update-rollouts", authn(auth.RequirePermission("system:manage")(create)))
	mux.Handle("GET /api/v1/platform-update-rollouts/{rollout_id}", authn(auth.RequirePermission("servers:read")(get)))
	mux.Handle("POST /api/v1/platform-update-rollouts/{rollout_id}/advance", authn(auth.RequirePermission("system:manage")(advance)))
}
