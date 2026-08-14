package geoip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const ipWhoisEndpoint = "https://ipwho.is/"

type IPWhoisResolver struct {
	client *http.Client
}

func NewIPWhoisResolver(client *http.Client) *IPWhoisResolver {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &IPWhoisResolver{client: client}
}

type ipWhoisResponse struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	Country   string  `json:"country"`
	Region    string  `json:"region"`
	City      string  `json:"city"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (r *IPWhoisResolver) Lookup(ctx context.Context, rawIP string) (Location, error) {
	ip, err := normalizePublicIP(rawIP)
	if err != nil {
		return Location{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipWhoisEndpoint+ip+"?fields=success,message,country,region,city,latitude,longitude", nil)
	if err != nil {
		return Location{}, fmt.Errorf("build GeoIP request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RouteGate-Manager")

	resp, err := r.client.Do(req)
	if err != nil {
		return Location{}, fmt.Errorf("GeoIP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return Location{}, ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		return Location{}, fmt.Errorf("GeoIP provider returned HTTP %d", resp.StatusCode)
	}

	var payload ipWhoisResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 32*1024))
	if err := decoder.Decode(&payload); err != nil {
		return Location{}, fmt.Errorf("decode GeoIP response: %w", err)
	}
	if !payload.Success {
		if payload.Message == "" {
			payload.Message = "lookup failed"
		}
		return Location{}, errors.New(payload.Message)
	}
	if math.IsNaN(payload.Latitude) || math.IsNaN(payload.Longitude) || math.IsInf(payload.Latitude, 0) || math.IsInf(payload.Longitude, 0) || payload.Latitude < -90 || payload.Latitude > 90 || payload.Longitude < -180 || payload.Longitude > 180 {
		return Location{}, errors.New("GeoIP provider returned invalid coordinates")
	}

	return Location{
		Country:   payload.Country,
		Region:    payload.Region,
		City:      payload.City,
		Latitude:  payload.Latitude,
		Longitude: payload.Longitude,
	}, nil
}
