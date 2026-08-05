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
	return checkVLESSListener(
		ctx,
		configPath,
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
		if !strings.EqualFold(strings.TrimSpace(typeName), "vless") {
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
		return ListenerHealthResult{}, fmt.Errorf("active sing-box config has no valid VLESS listen_port")
	}
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
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}

	if lastErr == nil {
		lastErr = checkCtx.Err()
	}
	return ListenerHealthResult{Port: port}, fmt.Errorf("VLESS listener on port %d is unreachable: %w", port, lastErr)
}
