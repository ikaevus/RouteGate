package geoip

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/servers"
)

type fakeServerRepository struct {
	items   []servers.Server
	updates []servers.UpdateServerGeographyInput
	ids     []string
}

func (r *fakeServerRepository) ListServers(_ context.Context, filter servers.ServerFilter) ([]servers.Server, error) {
	if filter.Offset >= len(r.items) {
		return []servers.Server{}, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(r.items) {
		end = len(r.items)
	}
	return r.items[filter.Offset:end], nil
}

func (r *fakeServerRepository) UpdateServerGeography(_ context.Context, id string, input servers.UpdateServerGeographyInput) (servers.Server, error) {
	r.ids = append(r.ids, id)
	r.updates = append(r.updates, input)
	return servers.Server{ID: id}, nil
}

type fakeResolver struct {
	calls []string
}

func (r *fakeResolver) Lookup(_ context.Context, ip string) (Location, error) {
	r.calls = append(r.calls, ip)
	return Location{Country: "United States", Region: "Virginia", City: "Ashburn", Latitude: 39.0438, Longitude: -77.4874}, nil
}

func TestWorkerDetectsOnlyUnlocatedNonManualServers(t *testing.T) {
	manualLat, manualLon := 55.7558, 37.6173
	autoLat, autoLon := 40.7128, -74.0060
	repo := &fakeServerRepository{items: []servers.Server{
		{ID: "auto", PublicIP: "8.8.8.8"},
		{ID: "manual", PublicIP: "1.1.1.1", LocationSource: servers.LocationSourceManual, LocationLatitude: &manualLat, LocationLongitude: &manualLon},
		{ID: "already-auto", PublicIP: "9.9.9.9", LocationSource: servers.LocationSourceAutoDetected, LocationLatitude: &autoLat, LocationLongitude: &autoLon},
		{ID: "no-public-ip"},
	}}
	resolver := &fakeResolver{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker := NewWorker(logger, repo, resolver)

	if err := worker.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(resolver.calls) != 1 || resolver.calls[0] != "8.8.8.8" {
		t.Fatalf("unexpected resolver calls: %#v", resolver.calls)
	}
	if len(repo.updates) != 1 || len(repo.ids) != 1 || repo.ids[0] != "auto" {
		t.Fatalf("unexpected updates: ids=%#v updates=%#v", repo.ids, repo.updates)
	}
	if repo.updates[0].Source != servers.LocationSourceAutoDetected {
		t.Fatalf("source = %q, want auto_detected", repo.updates[0].Source)
	}
	if repo.updates[0].Latitude == nil || repo.updates[0].Longitude == nil {
		t.Fatal("automatic location must include coordinates")
	}
}
