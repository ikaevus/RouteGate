package app

import (
	"context"
	"log/slog"

	"github.com/artuazh/routegate/backend/internal/config"
	routegatehttp "github.com/artuazh/routegate/backend/internal/http"
)

type App struct {
	cfg    config.Config
	logger *slog.Logger
	server *routegatehttp.Server
}

func New(cfg config.Config, logger *slog.Logger) *App {
	return &App{
		cfg:    cfg,
		logger: logger,
		server: routegatehttp.NewServer(cfg, logger),
	}
}

func (a *App) Start(ctx context.Context) error {
	a.logger.Info("starting RouteGate Manager", "env", a.cfg.Env, "addr", a.cfg.HTTPAddr)
	return a.server.Start(ctx)
}

func (a *App) Stop(ctx context.Context) error {
	a.logger.Info("stopping RouteGate Manager")
	return a.server.Stop(ctx)
}
