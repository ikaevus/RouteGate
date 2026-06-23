package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ikaevus/routegate/backend/internal/app"
	"github.com/ikaevus/routegate/backend/internal/config"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	application := app.New(cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
