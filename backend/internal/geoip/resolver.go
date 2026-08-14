package geoip

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

var (
	ErrNotPublicIP = errors.New("IP address is not publicly routable")
	ErrRateLimited = errors.New("GeoIP provider rate limit exceeded")
)

type Location struct {
	Country   string
	Region    string
	City      string
	Latitude  float64
	Longitude float64
}

type Resolver interface {
	Lookup(context.Context, string) (Location, error)
}

func normalizePublicIP(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	address, err := netip.ParseAddr(raw)
	if err != nil {
		prefix, prefixErr := netip.ParsePrefix(raw)
		if prefixErr != nil || prefix.Bits() != prefix.Addr().BitLen() {
			return "", fmt.Errorf("parse IP address: %w", err)
		}
		address = prefix.Addr()
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return "", ErrNotPublicIP
	}
	return address.String(), nil
}
