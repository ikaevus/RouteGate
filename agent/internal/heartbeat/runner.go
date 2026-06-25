package heartbeat

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ikaevus/routegate/agent/internal/client"
	"github.com/ikaevus/routegate/agent/internal/config"
	"github.com/ikaevus/routegate/agent/internal/systeminfo"
)

type Runner struct {
	cfg        config.Config
	configPath string
	client     *client.Client
	logger     *slog.Logger
}

func NewRunner(cfg config.Config, configPath string, logger *slog.Logger) *Runner {
	return &Runner{cfg: cfg, configPath: configPath, client: client.New(cfg.ManagerURL), logger: logger}
}

func (r *Runner) Run(ctx context.Context, once bool) error {
	if err := r.ensureRegistered(ctx); err != nil {
		return err
	}
	if once {
		return r.sendHeartbeat(ctx)
	}
	if err := r.sendHeartbeat(ctx); err != nil {
		r.logger.Warn("heartbeat failed", "error", err)
	}
	ticker := time.NewTicker(r.cfg.HeartbeatInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.sendHeartbeat(ctx); err != nil {
				r.logger.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

func (r *Runner) ensureRegistered(ctx context.Context) error {
	if r.cfg.HasAgentCredentials() {
		return nil
	}
	if !r.cfg.HasRegistrationToken() {
		return errors.New("agent_token or registration_token is required")
	}
	info := systeminfo.Collect()
	res, err := r.client.Register(ctx, r.cfg, info)
	if err != nil {
		return err
	}
	r.cfg.AgentID = res.AgentID
	r.cfg.ServerID = res.ServerID
	r.cfg.AgentToken = res.AgentToken
	r.cfg.RegistrationToken = ""
	if err := r.cfg.Save(r.configPath); err != nil {
		return err
	}
	r.logger.Info("agent registered", "agent_id", r.cfg.AgentID, "server_id", r.cfg.ServerID)
	return nil
}

func (r *Runner) sendHeartbeat(ctx context.Context) error {
	info := systeminfo.Collect()
	res, err := r.client.Heartbeat(ctx, r.cfg.AgentToken, info)
	if err != nil {
		return err
	}
	r.logger.Info("heartbeat accepted", "agent_id", res.AgentID, "server_id", res.ServerID, "server_status", res.ServerStatus)
	return nil
}
