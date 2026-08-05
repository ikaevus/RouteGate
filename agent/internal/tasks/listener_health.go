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

const defaultListenerHealthTimeout = 3 * time.Second

type ListenerHealthResult struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func CheckVLESSListener(ctx context.Context, configPath string) (ListenerHealthResult, error) {
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

	checkCtx, cancel := context.WithTimeout(ctx, defaultListenerHealthTimeout)
	defer cancel()

	dialer := net.Dialer{}
	addresses := []string{
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		net.JoinHostPort("::1", strconv.Itoa(port)),
	}
	var lastErr error
	for _, address := range addresses {
		connection, dialErr := dialer.DialContext(checkCtx, "tcp", address)
		if dialErr == nil {
			_ = connection.Close()
			return ListenerHealthResult{Address: address, Port: port}, nil
		}
		lastErr = dialErr
	}
	return ListenerHealthResult{Port: port}, fmt.Errorf("VLESS listener on port %d is unreachable: %w", port, lastErr)
}
