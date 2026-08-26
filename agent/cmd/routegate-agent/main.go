package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ikaevus/routegate/agent/internal/config"
	"github.com/ikaevus/routegate/agent/internal/heartbeat"
	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to YAML agent config")
	once := flag.Bool("once", false, "send one heartbeat and exit")
	platformUpdateWorkerTask := flag.String("platform-update-worker-task", "", "internal detached VPN update worker task UUID")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if *platformUpdateWorkerTask != "" {
		if err := tasks.RunPlatformUpdateWorker(*platformUpdateWorkerTask); err != nil {
			logger.Error("platform update worker failed", "error", err)
			os.Exit(1)
		}
		return
	}

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
