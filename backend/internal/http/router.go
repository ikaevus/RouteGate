package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/health"
	"github.com/ikaevus/routegate/backend/internal/portal"
	"github.com/ikaevus/routegate/backend/internal/roles"
	"github.com/ikaevus/routegate/backend/internal/servers"
	"github.com/ikaevus/routegate/backend/internal/traffic"
	"github.com/ikaevus/routegate/backend/internal/users"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func NewRouter(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool) stdhttp.Handler {
	mux := stdhttp.NewServeMux()

	healthHandler := health.NewHandler(logger)
	authRepo := auth.NewRepository(pool)
	authHandler := auth.NewHandler(logger, pool, cfg.AuthSessionTTL)
	serversHandler := servers.NewHandler(logger, pool)
	agentsHandler := agents.NewHandler(logger, pool)
	configsHandler := configs.NewHandler(logger, pool)
	usersHandler := users.NewHandler(logger, pool)
	rolesHandler := roles.NewHandler(logger, pool)
	vpnAccountsHandler := vpnaccounts.NewHandler(logger, pool)
	trafficHandler := traffic.NewHandler(logger, pool)
	portalHandler := portal.NewHandler(logger, pool)
	authn := auth.Middleware(authRepo)
	portalAuth := func(handler stdhttp.HandlerFunc) stdhttp.Handler {
		return authn(auth.RequirePermission("portal:access")(stdhttp.HandlerFunc(handler)))
	}

	mux.HandleFunc("GET /api/admin/health", healthHandler.Get)
	mux.HandleFunc("GET /api/agent/health", healthHandler.Get)

	mux.HandleFunc("POST /api/admin/auth/login", authHandler.Login)
	mux.Handle("POST /api/admin/auth/logout", authn(stdhttp.HandlerFunc(authHandler.Logout)))
	mux.Handle("GET /api/admin/me", authn(stdhttp.HandlerFunc(authHandler.Me)))
	mux.Handle("GET /api/v1/auth/me", authn(stdhttp.HandlerFunc(authHandler.Me)))
	mux.Handle("POST /api/v1/auth/login", stdhttp.HandlerFunc(authHandler.Login))
	mux.Handle("POST /api/v1/auth/logout", authn(stdhttp.HandlerFunc(authHandler.Logout)))

	mux.Handle("GET /api/portal/me", portalAuth(portalHandler.Me))
	mux.Handle("GET /api/portal/dashboard", portalAuth(portalHandler.Dashboard))
	mux.Handle("GET /api/portal/profiles", portalAuth(portalHandler.ListProfiles))
	mux.Handle("GET /api/portal/profiles/{id}", portalAuth(portalHandler.GetProfile))
	mux.Handle("GET /api/portal/profiles/{id}/subscription", portalAuth(portalHandler.GetSubscription))
	mux.Handle("POST /api/portal/profiles/{id}/subscription-token", portalAuth(portalHandler.GenerateSubscriptionAccess))
	mux.Handle("GET /api/portal/profiles/{id}/qr", portalAuth(portalHandler.GetQRCode))
	mux.Handle("GET /api/portal/instructions", portalAuth(portalHandler.ListInstructions))
	mux.Handle("GET /api/portal/instructions/{platform}", portalAuth(portalHandler.GetInstruction))

	mux.HandleFunc("GET /api/admin/servers", serversHandler.LegacyList)
	mux.HandleFunc("GET /api/admin/servers/{id}", serversHandler.LegacyGet)
	mux.HandleFunc("POST /api/admin/servers", serversHandler.LegacyCreate)
	mux.Handle("GET /api/v1/servers", authn(auth.RequirePermission("servers:read")(stdhttp.HandlerFunc(serversHandler.List))))
	mux.Handle("POST /api/v1/servers", authn(auth.RequirePermission("servers:create")(stdhttp.HandlerFunc(serversHandler.Create))))
	mux.Handle("GET /api/v1/servers/{server_id}", authn(auth.RequirePermission("servers:read")(stdhttp.HandlerFunc(serversHandler.Get))))
	mux.Handle("PATCH /api/v1/servers/{server_id}", authn(auth.RequirePermission("servers:update")(stdhttp.HandlerFunc(serversHandler.Update))))
	mux.Handle("DELETE /api/v1/servers/{server_id}", authn(auth.RequirePermission("servers:delete")(stdhttp.HandlerFunc(serversHandler.Delete))))
	mux.Handle("GET /api/v1/servers/{server_id}/protocol-settings", authn(auth.RequirePermission("servers:read")(stdhttp.HandlerFunc(serversHandler.GetProtocolSettings))))
	mux.Handle("PATCH /api/v1/servers/{server_id}/protocol-settings", authn(auth.RequirePermission("servers:update")(stdhttp.HandlerFunc(serversHandler.UpdateProtocolSettings))))
	mux.Handle("POST /api/v1/servers/{server_id}/protocol-settings/reality-keypair", authn(auth.RequirePermission("servers:update")(stdhttp.HandlerFunc(serversHandler.GenerateRealityKeypair))))
	mux.Handle("POST /api/v1/servers/{server_id}/registration-token", authn(auth.RequirePermission("agents:register")(stdhttp.HandlerFunc(serversHandler.CreateRegistrationToken))))
	mux.Handle("POST /api/v1/servers/{server_id}/config/render", authn(auth.RequirePermission("configs:render")(stdhttp.HandlerFunc(configsHandler.Render))))
	mux.Handle("GET /api/v1/servers/{server_id}/config/versions", authn(auth.RequirePermission("configs:read")(stdhttp.HandlerFunc(configsHandler.List))))
	mux.Handle("GET /api/v1/servers/{server_id}/config/versions/{version_id}", authn(auth.RequirePermission("configs:read")(stdhttp.HandlerFunc(configsHandler.Get))))
	mux.Handle("POST /api/v1/servers/{server_id}/config/versions/{version_id}/validate", authn(auth.RequirePermission("configs:validate")(stdhttp.HandlerFunc(configsHandler.Validate))))
	mux.Handle("POST /api/v1/servers/{server_id}/config/versions/{version_id}/apply", authn(auth.RequirePermission("configs:apply")(stdhttp.HandlerFunc(configsHandler.Apply))))
	mux.Handle("GET /api/v1/servers/{server_id}/config/apply-jobs", authn(auth.RequirePermission("configs:read")(stdhttp.HandlerFunc(configsHandler.ListApplyJobs))))
	mux.Handle("GET /api/v1/servers/{server_id}/config/apply-jobs/{job_id}", authn(auth.RequirePermission("configs:read")(stdhttp.HandlerFunc(configsHandler.GetApplyJob))))

	mux.HandleFunc("GET /api/admin/agents", agentsHandler.List)
	mux.Handle("GET /api/v1/agents", authn(auth.RequirePermission("agents:read")(stdhttp.HandlerFunc(agentsHandler.List))))

	mux.Handle("GET /api/v1/vpn-accounts", authn(auth.RequirePermission("vpn_users:read")(stdhttp.HandlerFunc(vpnAccountsHandler.List))))
	mux.Handle("POST /api/v1/vpn-accounts", authn(auth.RequirePermission("vpn_users:create")(stdhttp.HandlerFunc(vpnAccountsHandler.Create))))
	mux.Handle("GET /api/v1/vpn-accounts/{id}", authn(auth.RequirePermission("vpn_users:read")(stdhttp.HandlerFunc(vpnAccountsHandler.Get))))
	mux.Handle("GET /api/v1/vpn-accounts/{id}/credentials", authn(auth.RequirePermission("vpn_users:read")(stdhttp.HandlerFunc(vpnAccountsHandler.GetCredentials))))
	mux.Handle("GET /api/v1/vpn-accounts/{id}/traffic", authn(auth.RequirePermission("traffic:read")(stdhttp.HandlerFunc(trafficHandler.GetAccountUsage))))
	mux.Handle("PATCH /api/v1/vpn-accounts/{id}/traffic-limit", authn(auth.RequirePermission("vpn_users:update")(stdhttp.HandlerFunc(trafficHandler.UpdateAccountLimit))))
	mux.Handle("PATCH /api/v1/vpn-accounts/{id}", authn(auth.RequirePermission("vpn_users:update")(stdhttp.HandlerFunc(vpnAccountsHandler.Update))))
	mux.Handle("DELETE /api/v1/vpn-accounts/{id}", authn(auth.RequirePermission("vpn_users:disable")(stdhttp.HandlerFunc(vpnAccountsHandler.Delete))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/suspend", authn(auth.RequirePermission("vpn_users:disable")(stdhttp.HandlerFunc(vpnAccountsHandler.Suspend))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/activate", authn(auth.RequirePermission("vpn_users:update")(stdhttp.HandlerFunc(vpnAccountsHandler.Activate))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/revoke", authn(auth.RequirePermission("vpn_users:disable")(stdhttp.HandlerFunc(vpnAccountsHandler.Revoke))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/subscription-token", authn(auth.RequirePermission("vpn_users:update")(stdhttp.HandlerFunc(vpnAccountsHandler.CreateSubscriptionToken))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/subscription-token/rotate", authn(auth.RequirePermission("vpn_users:update")(stdhttp.HandlerFunc(vpnAccountsHandler.RotateSubscriptionToken))))
	mux.Handle("DELETE /api/v1/vpn-accounts/{id}/subscription-token", authn(auth.RequirePermission("vpn_users:disable")(stdhttp.HandlerFunc(vpnAccountsHandler.RevokeSubscriptionToken))))
	mux.Handle("GET /api/v1/vpn-accounts/{id}/qr", authn(auth.RequirePermission("vpn_users:read")(stdhttp.HandlerFunc(vpnAccountsHandler.GetSubscriptionQRCode))))
	mux.HandleFunc("GET /api/v1/subscriptions/{token}", vpnAccountsHandler.GetPublicSubscription)

	mux.Handle("GET /api/v1/users", authn(auth.RequirePermission("users:read")(stdhttp.HandlerFunc(usersHandler.List))))
	mux.Handle("GET /api/v1/users/{id}", authn(auth.RequirePermission("users:read")(stdhttp.HandlerFunc(usersHandler.Get))))
	mux.Handle("POST /api/v1/users", authn(auth.RequirePermission("users:create")(stdhttp.HandlerFunc(usersHandler.Create))))
	mux.Handle("PATCH /api/v1/users/{id}", authn(auth.RequirePermission("users:update")(stdhttp.HandlerFunc(usersHandler.Update))))
	mux.Handle("POST /api/v1/users/{id}/disable", authn(auth.RequirePermission("users:disable")(stdhttp.HandlerFunc(usersHandler.Disable))))
	mux.Handle("POST /api/v1/users/{id}/enable", authn(auth.RequirePermission("users:disable")(stdhttp.HandlerFunc(usersHandler.Enable))))
	mux.Handle("GET /api/v1/roles", authn(auth.RequirePermission("roles:read")(stdhttp.HandlerFunc(rolesHandler.ListRoles))))
	mux.Handle("GET /api/v1/permissions", authn(auth.RequirePermission("roles:read")(stdhttp.HandlerFunc(rolesHandler.ListPermissions))))
	mux.HandleFunc("POST /api/v1/agent/register", agentsHandler.Register)
	mux.HandleFunc("POST /api/v1/agent/heartbeat", agentsHandler.Heartbeat)
	mux.HandleFunc("GET /api/v1/agent/tasks/next", agentsHandler.NextTask)
	mux.HandleFunc("POST /api/v1/agent/tasks/{job_id}/result", agentsHandler.CompleteTask)
	mux.HandleFunc("POST /api/v1/agent/traffic-usage", trafficHandler.ReportUsage)
	mux.HandleFunc("POST /api/v1/agent/traffic-reports", trafficHandler.ReportUsage)

	return chain(
		mux,
		recoverMiddleware(logger),
		requestIDMiddleware(),
		corsMiddleware(),
		loggingMiddleware(logger),
	)
}
