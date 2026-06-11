package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/artuazh/routegate/backend/internal/agents"
	"github.com/artuazh/routegate/backend/internal/auth"
	"github.com/artuazh/routegate/backend/internal/config"
	"github.com/artuazh/routegate/backend/internal/health"
	"github.com/artuazh/routegate/backend/internal/roles"
	"github.com/artuazh/routegate/backend/internal/servers"
	"github.com/artuazh/routegate/backend/internal/users"
)

func NewRouter(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool) stdhttp.Handler {
	mux := stdhttp.NewServeMux()

	healthHandler := health.NewHandler(logger)
	authRepo := auth.NewRepository(pool)
	authHandler := auth.NewHandler(logger, pool, cfg.AuthSessionTTL)
	serversHandler := servers.NewHandler(logger, pool)
	agentsHandler := agents.NewHandler(logger, pool)
	usersHandler := users.NewHandler(logger, pool)
	rolesHandler := roles.NewHandler(logger, pool)
	authn := auth.Middleware(authRepo)

	mux.HandleFunc("GET /api/admin/health", healthHandler.Get)
	mux.HandleFunc("GET /api/agent/health", healthHandler.Get)

	mux.HandleFunc("POST /api/admin/auth/login", authHandler.Login)
	mux.Handle("POST /api/admin/auth/logout", authn(stdhttp.HandlerFunc(authHandler.Logout)))
	mux.Handle("GET /api/admin/me", authn(stdhttp.HandlerFunc(authHandler.Me)))
	mux.Handle("GET /api/v1/auth/me", authn(stdhttp.HandlerFunc(authHandler.Me)))
	mux.Handle("POST /api/v1/auth/login", stdhttp.HandlerFunc(authHandler.Login))
	mux.Handle("POST /api/v1/auth/logout", authn(stdhttp.HandlerFunc(authHandler.Logout)))

	mux.HandleFunc("GET /api/admin/servers", serversHandler.LegacyList)
	mux.HandleFunc("POST /api/admin/servers", serversHandler.LegacyCreate)
	mux.Handle("GET /api/v1/servers", authn(auth.RequirePermission("servers:read")(stdhttp.HandlerFunc(serversHandler.List))))
	mux.Handle("POST /api/v1/servers", authn(auth.RequirePermission("servers:create")(stdhttp.HandlerFunc(serversHandler.Create))))
	mux.Handle("GET /api/v1/servers/{server_id}", authn(auth.RequirePermission("servers:read")(stdhttp.HandlerFunc(serversHandler.Get))))
	mux.Handle("PATCH /api/v1/servers/{server_id}", authn(auth.RequirePermission("servers:update")(stdhttp.HandlerFunc(serversHandler.Update))))
	mux.Handle("DELETE /api/v1/servers/{server_id}", authn(auth.RequirePermission("servers:delete")(stdhttp.HandlerFunc(serversHandler.Delete))))
	mux.Handle("POST /api/v1/servers/{server_id}/registration-token", authn(auth.RequirePermission("agents:register")(stdhttp.HandlerFunc(serversHandler.CreateRegistrationToken))))

	mux.HandleFunc("GET /api/admin/agents", agentsHandler.List)
	mux.Handle("GET /api/v1/agents", authn(auth.RequirePermission("agents:read")(stdhttp.HandlerFunc(agentsHandler.List))))

	mux.Handle("GET /api/v1/users", authn(auth.RequirePermission("users:read")(stdhttp.HandlerFunc(usersHandler.List))))
	mux.Handle("GET /api/v1/users/{id}", authn(auth.RequirePermission("users:read")(stdhttp.HandlerFunc(usersHandler.Get))))
	mux.Handle("POST /api/v1/users", authn(auth.RequirePermission("users:create")(stdhttp.HandlerFunc(usersHandler.Create))))
	mux.Handle("PATCH /api/v1/users/{id}", authn(auth.RequirePermission("users:update")(stdhttp.HandlerFunc(usersHandler.Update))))
	mux.Handle("POST /api/v1/users/{id}/disable", authn(auth.RequirePermission("users:disable")(stdhttp.HandlerFunc(usersHandler.Disable))))
	mux.Handle("POST /api/v1/users/{id}/enable", authn(auth.RequirePermission("users:disable")(stdhttp.HandlerFunc(usersHandler.Enable))))
	mux.Handle("GET /api/v1/roles", authn(auth.RequirePermission("roles:read")(stdhttp.HandlerFunc(rolesHandler.ListRoles))))
	mux.Handle("GET /api/v1/permissions", authn(auth.RequirePermission("roles:read")(stdhttp.HandlerFunc(rolesHandler.ListPermissions))))
	mux.HandleFunc("POST /api/agent/register", agentsHandler.Register)
	mux.HandleFunc("POST /api/agent/heartbeat", agentsHandler.Heartbeat)

	return chain(
		mux,
		recoverMiddleware(logger),
		requestIDMiddleware(),
		corsMiddleware(),
		loggingMiddleware(logger),
	)
}
