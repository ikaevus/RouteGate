package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/artuazh/routegate/agent/internal/config"
	"github.com/artuazh/routegate/agent/internal/heartbeat"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := heartbeat.NewRunner(cfg, logger)
	if err := runner.Run(ctx); err != nil {
		logger.Error("routegate agent stopped with error", "error", err)
		os.Exit(1)
	}
}
