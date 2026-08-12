package delivery

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

func NewConfiguredWorker(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool) *Worker {
	repository := NewRepository(pool)
	recorder := audit.NewRecorder(logger, pool)
	providers := NewProviderSettingsManager(pool, cfg, logger)
	resolver := NewVPNAccessResolver(vpnaccounts.NewRepository(pool), cfg.PublicURL)
	return NewWorker(repository, resolver, NewRenderer(), providers, recorder, logger)
}
