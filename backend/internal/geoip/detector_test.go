package geoip

import (
	"context"
	"errors"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/servers"
)

type fakeDetectionRepository struct {
	server  servers.Server
	updated servers.UpdateServerGeographyInput
}

func (f *fakeDetectionRepository) GetServerByID(context.Context, string) (servers.Server, error) {
	return f.server, nil
}

func (f *fakeDetectionRepository) UpdateServerGeography(_ context.Context, _ string, input servers.UpdateServerGeographyInput) (servers.Server, error) {
	f.updated = input
	return f.server, nil
}

type fakeResolver struct {
	location Location
	err      error
}

func (f fakeResolver) Lookup(context.Context, string) (Location, error) {
	return f.location, f.err
}

func TestDetectorOverwritesManualCoordinatesOnlyWhenExplicitlyRequested(t *testing.T) {
	oldLatitude := 50.1109
	oldLongitude := 8.6821
	repo := &fakeDetectionRepository{server: servers.Server{
		ID:                    "server-id",
		PublicIP:              "203.0.113.10",
		LocationLatitude:      &oldLatitude,
		LocationLongitude:     &oldLongitude,
		LocationSource:        servers.LocationSourceManual,
	}}
	detector := NewDetector(repo, fakeResolver{location: Location{
		Country: "United States", Region: "Virginia", City: "Ashburn",
		Latitude: 39.0438, Longitude: -77.4874,
	}})

	if _, err := detector.Detect(context.Background(), "server-id"); err != nil {
		t.Fatalf("detect location: %v", err)
	}
	if repo.updated.Source != servers.LocationSourceAutoDetected {
		t.Fatalf("source=%q, want %q", repo.updated.Source, servers.LocationSourceAutoDetected)
	}
	if repo.updated.Latitude == nil || repo.updated.Longitude == nil {
		t.Fatal("expected detected coordinates")
	}
	if *repo.updated.Latitude != 39.0438 || *repo.updated.Longitude != -77.4874 {
		t.Fatalf("coordinates=%v,%v", *repo.updated.Latitude, *repo.updated.Longitude)
	}
}

func TestDetectorRequiresPublicIP(t *testing.T) {
	repo := &fakeDetectionRepository{server: servers.Server{ID: "server-id"}}
	detector := NewDetector(repo, fakeResolver{})

	_, err := detector.Detect(context.Background(), "server-id")
	if !errors.Is(err, ErrPublicIPRequired) {
		t.Fatalf("expected ErrPublicIPRequired, got %v", err)
	}
}
