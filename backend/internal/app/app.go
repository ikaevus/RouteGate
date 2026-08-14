package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/db"
	"github.com/ikaevus/routegate/backend/internal/delivery"
	routegatehttp "github.com/ikaevus/routegate/backend/internal/http"
	"github.com/ikaevus/routegate/backend/internal/observability"
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
	if err := delivery.EnsureProviderSecretStore(ctx, pool, a.cfg, a.logger); err != nil {
		return err
	}

	authRepo := auth.NewRepository(pool)
	if err := authRepo.EnsureBuiltIns(ctx); err != nil {
		return err
	}
	hasSuperAdmin, err := authRepo.HasSuperAdmin(ctx)
	if err != nil {
		return err
	}
	if !hasSuperAdmin {
		if a.cfg.BootstrapAdminEmail == "" || a.cfg.BootstrapAdminPassword == "" {
			a.logger.Warn("no super_admin user exists; set ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL and ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD to bootstrap the first SuperAdmin")
		} else if err := authRepo.CreateBootstrapSuperAdmin(ctx, a.cfg.BootstrapAdminEmail, a.cfg.BootstrapAdminUsername, a.cfg.BootstrapAdminPassword, a.cfg.BootstrapAdminDisplayName); err != nil {
			return err
		} else {
			a.logger.Info("bootstrapped first SuperAdmin", "email", a.cfg.BootstrapAdminEmail)
		}
	}

	a.server = routegatehttp.NewServer(a.cfg, a.logger, pool)
	deliveryWorker := delivery.NewConfiguredWorker(a.cfg, a.logger, pool)
	healthWorker := observability.NewHealthWorker(a.logger, pool)
	alertWorker := observability.NewAlertEngine(a.logger, pool)
	notificationWorker := observability.NewNotificationWorker(
		observability.NewNotificationRepository(pool),
		delivery.NewConfiguredSystemNotificationCreator(a.logger, pool),
		a.logger,
	)
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 5)
	go func() { errCh <- a.server.Start(runtimeCtx) }()
	go func() { errCh <- deliveryWorker.Run(runtimeCtx) }()
	go func() { errCh <- healthWorker.Run(runtimeCtx) }()
	go func() { errCh <- alertWorker.Run(runtimeCtx) }()
	go func() { errCh <- notificationWorker.Run(runtimeCtx) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		cancel()
		return err
	}
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
