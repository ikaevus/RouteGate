package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/servers"
)

func TestServerPublicIPChangeInvalidatesOnlyAutomaticGeography(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var automaticID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name,status,public_ip,
			location_country,location_region,location_city,
			location_latitude,location_longitude,location_source
		) VALUES (
			'Automatic Node','active','8.8.8.8'::inet,
			'Old Country','Old Region','Old City',
			10.0,20.0,'auto_detected'
		) RETURNING id::text
	`).Scan(&automaticID); err != nil {
		t.Fatalf("create automatic server: %v", err)
	}

	var manualID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name,status,public_ip,
			location_country,location_region,location_city,
			location_latitude,location_longitude,location_source
		) VALUES (
			'Manual Node','active','1.1.1.1'::inet,
			'Manual Country','Manual Region','Manual City',
			55.7558,37.6173,'manual'
		) RETURNING id::text
	`).Scan(&manualID); err != nil {
		t.Fatalf("create manual server: %v", err)
	}

	repository := servers.NewRepository(pool)
	newAutomaticIP := "9.9.9.9"
	automatic, err := repository.UpdateServer(ctx, automaticID, servers.UpdateServerInput{PublicIP: &newAutomaticIP})
	if err != nil {
		t.Fatalf("update automatic server public IP: %v", err)
	}
	if automatic.PublicIP != newAutomaticIP {
		t.Fatalf("automatic public IP=%q, want %q", automatic.PublicIP, newAutomaticIP)
	}
	if automatic.LocationSource != "" || automatic.LocationCountry != "" || automatic.LocationRegion != "" || automatic.LocationCity != "" || automatic.LocationLatitude != nil || automatic.LocationLongitude != nil {
		t.Fatalf("automatic geography must be invalidated after public IP change: %+v", automatic)
	}

	newManualIP := "8.8.4.4"
	manual, err := repository.UpdateServer(ctx, manualID, servers.UpdateServerInput{PublicIP: &newManualIP})
	if err != nil {
		t.Fatalf("update manual server public IP: %v", err)
	}
	if manual.LocationSource != servers.LocationSourceManual || manual.LocationCountry != "Manual Country" || manual.LocationRegion != "Manual Region" || manual.LocationCity != "Manual City" {
		t.Fatalf("manual geography must survive public IP changes: %+v", manual)
	}
	if manual.LocationLatitude == nil || manual.LocationLongitude == nil || *manual.LocationLatitude != 55.7558 || *manual.LocationLongitude != 37.6173 {
		t.Fatalf("manual coordinates changed unexpectedly: %+v", manual)
	}

	if _, err := repository.UpdateServerGeography(ctx, automaticID, servers.UpdateServerGeographyInput{
		Country:   "Current Country",
		Region:    "Current Region",
		City:      "Current City",
		Latitude:  float64Ptr(40.0),
		Longitude: float64Ptr(-70.0),
		Source:    servers.LocationSourceAutoDetected,
	}); err != nil {
		t.Fatalf("restore automatic geography: %v", err)
	}

	unchanged, err := repository.UpdateServer(ctx, automaticID, servers.UpdateServerInput{PublicIP: &newAutomaticIP})
	if err != nil {
		t.Fatalf("update automatic server with unchanged public IP: %v", err)
	}
	if unchanged.LocationSource != servers.LocationSourceAutoDetected || unchanged.LocationCity != "Current City" || unchanged.LocationLatitude == nil || unchanged.LocationLongitude == nil {
		t.Fatalf("unchanged public IP must preserve automatic geography: %+v", unchanged)
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}
