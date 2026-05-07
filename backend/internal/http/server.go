package http

import (
	"context"
	"errors"
	"log/slog"
	stdhttp "net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/artuazh/routegate/backend/internal/config"
)

type Server struct {
	cfg        config.Config
	logger     *slog.Logger
	httpServer *stdhttp.Server
}

func NewServer(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool) *Server {
	router := NewRouter(logger, pool)

	return &Server{
		cfg:    cfg,
		logger: logger,
		httpServer: &stdhttp.Server{
			Addr:    cfg.HTTPAddr,
			Handler: router,
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.logger.Info("HTTP server listening", "addr", s.cfg.HTTPAddr)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
