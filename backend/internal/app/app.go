package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/artuazh/routegate/backend/internal/config"
	"github.com/artuazh/routegate/backend/internal/db"
	routegatehttp "github.com/artuazh/routegate/backend/internal/http"
)

type App struct {
	cfg    config.Config
	logger *slog.Logger
	pool   *pgxpool.Pool
	server *routegatehttp.Server
}

func New(cfg config.Config, logger *slog.Logger) *App {
	return &App{
		cfg:    cfg,
		logger: logger,
	}
}

func (a *App) Start(ctx context.Context) error {
	a.logger.Info("starting RouteGate Manager", "env", a.cfg.Env, "addr", a.cfg.HTTPAddr)

	pool, err := db.Connect(ctx, a.cfg.DatabaseURL, a.logger)
	if err != nil {
		return err
	}

	a.pool = pool

	if err := db.Migrate(ctx, pool, "migrations", a.logger); err != nil {
		return err
	}

	a.server = routegatehttp.NewServer(a.cfg, a.logger, pool)

	return a.server.Start(ctx)
}

func (a *App) Stop(ctx context.Context) error {
	a.logger.Info("stopping RouteGate Manager")

	if a.server != nil {
		if err := a.server.Stop(ctx); err != nil {
			return err
		}
	}

	if a.pool != nil {
		a.pool.Close()
	}

	return nil
}
