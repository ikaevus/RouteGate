package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikaevus/routegate/backend/internal/app"
	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/db"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit without starting Manager")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	application := app.New(cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *migrateOnly {
		pool, err := db.Connect(ctx, cfg.DatabaseURL, logger)
		if err != nil {
			logger.Error("migration database connection failed", "error", err)
			os.Exit(1)
		}
		err = db.Migrate(ctx, pool, "migrations", logger)
		if err == nil {
			err = db.CheckSchema(ctx, pool, "migrations")
		}
		pool.Close()
		if err != nil {
			logger.Error("database migration failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := application.Start(ctx); err != nil {
		logger.Error("routegate manager stopped with error", "error", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := application.Stop(shutdownCtx); err != nil {
		logger.Error("routegate manager shutdown error", "error", err)
		os.Exit(1)
	}
}
