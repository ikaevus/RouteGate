package tasks

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultRealityHandshakeTimeout = 8 * time.Second

type realityHandshakeTarget struct {
	Server     string
	Port       int
	ServerName string
}

// CheckRealityHandshakeTargets verifies, from this node, that every Reality
// handshake target in a staged sing-box config resolves and completes a TLS 1.3
// handshake. sing-box check does not resolve names, so an NXDOMAIN or blocked
// target would otherwise pass validation and break every client after restart.
func CheckRealityHandshakeTargets(ctx context.Context, configPath string) error {
	return checkRealityHandshakeTargets(ctx, configPath, defaultRealityHandshakeTimeout, dialRealityHandshakeTarget)
}

func checkRealityHandshakeTargets(
	ctx context.Context,
	configPath string,
	timeout time.Duration,
	dial func(context.Context, realityHandshakeTarget) error,
) error {
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return fmt.Errorf("read staged sing-box config: %w", err)
	}
	targets, err := realityHandshakeTargets(payload)
	if err != nil {
		return err
	}
	for _, target := range targets {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		err := dial(checkCtx, target)
		cancel()
		if err != nil {
			return fmt.Errorf(
				"Reality handshake target %s is not usable from this node: %s. Choose an external TLS 1.3 site that resolves and answers from this VPN node, save it as the Reality server name, then render and apply a new version",
				net.JoinHostPort(target.Server, strconv.Itoa(target.Port)),
				describeRealityHandshakeError(err),
			)
		}
	}
	return nil
}

func realityHandshakeTargets(payload []byte) ([]realityHandshakeTarget, error) {
	var config struct {
		Inbounds []struct {
			TLS *struct {
				ServerName string `json:"server_name"`
				Reality    *struct {
					Enabled   bool `json:"enabled"`
					Handshake struct {
						Server     string `json:"server"`
						ServerPort int    `json:"server_port"`
					} `json:"handshake"`
				} `json:"reality"`
			} `json:"tls"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		return nil, fmt.Errorf("decode staged sing-box config: %w", err)
	}
	targets := []realityHandshakeTarget{}
	for _, inbound := range config.Inbounds {
		if inbound.TLS == nil || inbound.TLS.Reality == nil || !inbound.TLS.Reality.Enabled {
			continue
		}
		target := realityHandshakeTarget{
			Server:     strings.TrimSpace(inbound.TLS.Reality.Handshake.Server),
			Port:       inbound.TLS.Reality.Handshake.ServerPort,
			ServerName: strings.TrimSpace(inbound.TLS.ServerName),
		}
		if target.Server == "" {
			// sing-box check reports structural errors; nothing to dial.
			continue
		}
		if target.Port == 0 {
			target.Port = 443
		}
		if target.ServerName == "" {
			target.ServerName = target.Server
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func dialRealityHandshakeTarget(ctx context.Context, target realityHandshakeTarget) error {
	dialer := tls.Dialer{Config: &tls.Config{
		ServerName: target.ServerName,
		MinVersion: tls.VersionTLS13,
		// Reality forwards unauthenticated handshakes to this site; the node
		// needs reachability and TLS 1.3, not trust in the site's certificate.
		InsecureSkipVerify: true, //nolint:gosec // reachability probe only; no data is exchanged
	}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(target.Server, strconv.Itoa(target.Port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func describeRealityHandshakeError(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return "DNS name does not exist (NXDOMAIN)"
	case errors.As(err, &dnsErr):
		return "DNS lookup failed"
	case errors.Is(err, context.DeadlineExceeded):
		return "TCP or TLS handshake timed out"
	default:
		return strings.TrimSpace(err.Error())
	}
}
