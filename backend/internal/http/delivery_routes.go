package http

import (
	"log/slog"
	stdhttp "net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/delivery"
)

func NewRootHandler(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool) stdhttp.Handler {
	base := NewRouter(cfg, logger, pool)
	mux := stdhttp.NewServeMux()

	authRepo := auth.NewRepository(pool)
	authn := auth.Middleware(authRepo)
	deliveryHandler := delivery.NewHandler(logger, pool, cfg)

	mux.Handle("GET /api/v1/delivery/providers", authn(auth.RequirePermission("deliveries:read")(stdhttp.HandlerFunc(deliveryHandler.ListProviders))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/deliveries/preview", authn(auth.RequirePermission("deliveries:send")(stdhttp.HandlerFunc(deliveryHandler.PreviewForVPNAccount))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/deliveries", authn(auth.RequirePermission("deliveries:send")(stdhttp.HandlerFunc(deliveryHandler.CreateForVPNAccount))))
	mux.Handle("GET /api/v1/vpn-accounts/{id}/deliveries", authn(auth.RequirePermission("deliveries:read")(stdhttp.HandlerFunc(deliveryHandler.ListForVPNAccount))))
	mux.Handle("GET /api/v1/deliveries/{delivery_id}", authn(auth.RequirePermission("deliveries:read")(stdhttp.HandlerFunc(deliveryHandler.Get))))
	mux.Handle("POST /api/v1/deliveries/{delivery_id}/retry", authn(auth.RequirePermission("deliveries:send")(stdhttp.HandlerFunc(deliveryHandler.Retry))))
	mux.Handle("/", base)
	return mux
}
