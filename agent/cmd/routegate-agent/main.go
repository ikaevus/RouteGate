package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/artuazh/routegate/agent/internal/config"
	"github.com/artuazh/routegate/agent/internal/heartbeat"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to YAML agent config")
	once := flag.Bool("once", false, "send one heartbeat and exit")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := heartbeat.NewRunner(cfg, *configPath, logger)
	if err := runner.Run(ctx, *once); err != nil {
		logger.Error("routegate agent stopped with error", "error", err)
		os.Exit(1)
	}
}
