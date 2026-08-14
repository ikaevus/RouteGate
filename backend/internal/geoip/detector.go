package geoip

import (
	"context"
	"errors"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/servers"
)

var ErrPublicIPRequired = errors.New("server public IP is required for automatic geolocation")

type detectionRepository interface {
	GetServerByID(context.Context, string) (servers.Server, error)
	UpdateServerGeography(context.Context, string, servers.UpdateServerGeographyInput) (servers.Server, error)
}

type Detector struct {
	repo     detectionRepository
	resolver Resolver
}

func NewDetector(repo detectionRepository, resolver Resolver) *Detector {
	return &Detector{repo: repo, resolver: resolver}
}

func (d *Detector) Detect(ctx context.Context, serverID string) (servers.Server, error) {
	server, err := d.repo.GetServerByID(ctx, serverID)
	if err != nil {
		return servers.Server{}, err
	}
	if strings.TrimSpace(server.PublicIP) == "" {
		return servers.Server{}, ErrPublicIPRequired
	}

	location, err := d.resolver.Lookup(ctx, server.PublicIP)
	if err != nil {
		return servers.Server{}, err
	}

	latitude := location.Latitude
	longitude := location.Longitude
	return d.repo.UpdateServerGeography(ctx, server.ID, servers.UpdateServerGeographyInput{
		Country:   location.Country,
		Region:    location.Region,
		City:      location.City,
		Latitude:  &latitude,
		Longitude: &longitude,
		Source:    servers.LocationSourceAutoDetected,
	})
}
