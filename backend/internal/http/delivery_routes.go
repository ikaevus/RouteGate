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
	mux.Handle("GET /api/v1/delivery/providers/{provider}/settings", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.GetProviderSettings))))
	mux.Handle("PUT /api/v1/delivery/providers/{provider}/settings", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.PutProviderSettings))))
	mux.Handle("POST /api/v1/delivery/providers/{provider}/settings/test", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.TestProviderSettings))))
	mux.Handle("DELETE /api/v1/delivery/providers/{provider}/settings", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.DeleteProviderSettings))))

	mux.Handle("POST /api/v1/delivery/telegram/pairings", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.StartTelegramPairing))))
	mux.Handle("GET /api/v1/delivery/telegram/pairings/{pairing_id}", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.GetTelegramPairing))))
	mux.Handle("GET /api/v1/delivery/telegram/recipients", authn(auth.RequirePermission("deliveries:read")(stdhttp.HandlerFunc(deliveryHandler.ListTelegramRecipients))))
	mux.Handle("POST /api/v1/delivery/telegram/recipients/{recipient_id}/test", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.TestTelegramRecipient))))
	mux.Handle("DELETE /api/v1/delivery/telegram/recipients/{recipient_id}", authn(auth.RequirePermission("system:manage")(stdhttp.HandlerFunc(deliveryHandler.DeleteTelegramRecipient))))

	mux.Handle("POST /api/v1/vpn-accounts/{id}/deliveries/preview", authn(auth.RequirePermission("deliveries:send")(stdhttp.HandlerFunc(deliveryHandler.PreviewForVPNAccount))))
	mux.Handle("POST /api/v1/vpn-accounts/{id}/deliveries", authn(auth.RequirePermission("deliveries:send")(stdhttp.HandlerFunc(deliveryHandler.CreateForVPNAccount))))
	mux.Handle("GET /api/v1/vpn-accounts/{id}/deliveries", authn(auth.RequirePermission("deliveries:read")(stdhttp.HandlerFunc(deliveryHandler.ListForVPNAccount))))
	mux.Handle("GET /api/v1/deliveries/{delivery_id}", authn(auth.RequirePermission("deliveries:read")(stdhttp.HandlerFunc(deliveryHandler.Get))))
	mux.Handle("POST /api/v1/deliveries/{delivery_id}/retry", authn(auth.RequirePermission("deliveries:send")(stdhttp.HandlerFunc(deliveryHandler.Retry))))
	mux.Handle("/", base)
	return mux
}
