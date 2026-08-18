package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenerHealthTimeout       = 3 * time.Second
	defaultListenerHealthRetryInterval = 100 * time.Millisecond
)

type ListenerHealthResult struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func CheckVLESSListener(ctx context.Context, configPath string) (ListenerHealthResult, error) {
	return CheckSingBoxTCPListener(ctx, configPath, "vless")
}

func CheckSingBoxTCPListener(ctx context.Context, configPath, inboundType string) (ListenerHealthResult, error) {
	return checkSingBoxTCPListener(
		ctx,
		configPath,
		inboundType,
		defaultListenerHealthTimeout,
		defaultListenerHealthRetryInterval,
	)
}

func checkVLESSListener(
	ctx context.Context,
	configPath string,
	timeout time.Duration,
	retryInterval time.Duration,
) (ListenerHealthResult, error) {
	return checkSingBoxTCPListener(ctx, configPath, "vless", timeout, retryInterval)
}

func checkSingBoxTCPListener(
	ctx context.Context,
	configPath string,
	inboundType string,
	timeout time.Duration,
	retryInterval time.Duration,
) (ListenerHealthResult, error) {
	inboundType = strings.ToLower(strings.TrimSpace(inboundType))
	if inboundType == "" {
		return ListenerHealthResult{}, fmt.Errorf("sing-box inbound type is required")
	}
	payload, err := os.ReadFile(strings.TrimSpace(configPath))
	if err != nil {
		return ListenerHealthResult{}, fmt.Errorf("read active sing-box config: %w", err)
	}

	var config struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		return ListenerHealthResult{}, fmt.Errorf("decode active sing-box config: %w", err)
	}

	port := 0
	for _, inbound := range config.Inbounds {
		typeName, _ := inbound["type"].(string)
		if !strings.EqualFold(strings.TrimSpace(typeName), inboundType) {
			continue
		}
		switch value := inbound["listen_port"].(type) {
		case float64:
			port = int(value)
		case int:
			port = value
		case json.Number:
			parsed, parseErr := value.Int64()
			if parseErr == nil {
				port = int(parsed)
			}
		}
		break
	}
	if port < 1 || port > 65535 {
		return ListenerHealthResult{}, fmt.Errorf("active sing-box config has no valid %s listen_port", inboundType)
	}
	return checkTCPListenerPort(ctx, port, inboundType, timeout, retryInterval)
}

func checkTCPListenerPort(ctx context.Context, port int, label string, timeout, retryInterval time.Duration) (ListenerHealthResult, error) {
	if timeout <= 0 {
		timeout = defaultListenerHealthTimeout
	}
	if retryInterval <= 0 {
		retryInterval = defaultListenerHealthRetryInterval
	}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{}
	addresses := []string{
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		net.JoinHostPort("::1", strconv.Itoa(port)),
	}
	var lastErr error

	for {
		for _, address := range addresses {
			connection, dialErr := dialer.DialContext(checkCtx, "tcp", address)
			if dialErr == nil {
				_ = connection.Close()
				return ListenerHealthResult{Address: address, Port: port}, nil
			}
			lastErr = dialErr
			if checkCtx.Err() != nil {
				break
			}
		}

		if checkCtx.Err() != nil {
			break
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-checkCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}

	if lastErr == nil {
		lastErr = checkCtx.Err()
	}
	return ListenerHealthResult{Port: port}, fmt.Errorf("%s listener on port %d is unreachable: %w", label, port, lastErr)
}
