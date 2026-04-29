package heartbeat

import (
	"context"
	"log/slog"
	"time"

	"github.com/artuazh/routegate/agent/internal/config"
)

type Runner struct {
	cfg    config.Config
	logger *slog.Logger
}

func NewRunner(cfg config.Config, logger *slog.Logger) *Runner {
	return &Runner{cfg: cfg, logger: logger}
}

func (r *Runner) Run(ctx context.Context) error {
	r.logger.Info("starting RouteGate Agent", "agent_id", r.cfg.AgentID, "manager_url", r.cfg.ManagerURL)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("stopping RouteGate Agent")
			return nil
		case <-ticker.C:
			r.logger.Info("heartbeat tick", "agent_id", r.cfg.AgentID)
		}
	}
}
