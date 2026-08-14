package geoip

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ikaevus/routegate/backend/internal/servers"
)

const (
	defaultReconcileInterval = time.Hour
	serverPageSize           = 100
	maxLookupsPerRun         = 40
)

type serverRepository interface {
	ListServers(context.Context, servers.ServerFilter) ([]servers.Server, error)
	UpdateServerGeography(context.Context, string, servers.UpdateServerGeographyInput) (servers.Server, error)
}

type Worker struct {
	logger   *slog.Logger
	repo     serverRepository
	resolver Resolver
	interval time.Duration
}

func NewWorker(logger *slog.Logger, repo serverRepository, resolver Resolver) *Worker {
	return &Worker{
		logger:   logger,
		repo:     repo,
		resolver: resolver,
		interval: defaultReconcileInterval,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	w.reconcileAndLog(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.reconcileAndLog(ctx)
		}
	}
}

func (w *Worker) reconcileAndLog(ctx context.Context) {
	if err := w.reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Warn("automatic server geolocation reconciliation failed", "error", err)
	}
}

func (w *Worker) reconcile(ctx context.Context) error {
	lookups := 0
	for offset := 0; ; offset += serverPageSize {
		items, err := w.repo.ListServers(ctx, servers.ServerFilter{Limit: serverPageSize, Offset: offset})
		if err != nil {
			return err
		}

		for _, server := range items {
			if lookups >= maxLookupsPerRun {
				return nil
			}
			if !needsAutomaticLocation(server) {
				continue
			}

			lookups++
			location, err := w.resolver.Lookup(ctx, server.PublicIP)
			if errors.Is(err, ErrRateLimited) {
				w.logger.Warn("automatic server geolocation paused because provider rate limit was reached")
				return nil
			}
			if err != nil {
				w.logger.Debug("automatic server geolocation lookup failed", "server_id", server.ID, "error", err)
				continue
			}

			latitude := location.Latitude
			longitude := location.Longitude
			if _, err := w.repo.UpdateServerGeography(ctx, server.ID, servers.UpdateServerGeographyInput{
				Country:   location.Country,
				Region:    location.Region,
				City:      location.City,
				Latitude:  &latitude,
				Longitude: &longitude,
				Source:    servers.LocationSourceAutoDetected,
			}); err != nil {
				w.logger.Warn("failed to persist automatic server geolocation", "server_id", server.ID, "error", err)
				continue
			}
			w.logger.Info("server location detected automatically", "server_id", server.ID, "country", location.Country, "city", location.City)
		}

		if len(items) < serverPageSize {
			return nil
		}
	}
}

func needsAutomaticLocation(server servers.Server) bool {
	if server.PublicIP == "" || server.LocationSource == servers.LocationSourceManual {
		return false
	}
	return server.LocationLatitude == nil || server.LocationLongitude == nil
}
