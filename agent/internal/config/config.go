package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ManagerURL   string
	AgentID      string
	AgentToken   string
	PollInterval time.Duration
	LogLevel     slog.Level
}

func Load() Config {
	return Config{
		ManagerURL:   env("ROUTEGATE_AGENT_MANAGER_URL", "http://localhost:8080"),
		AgentID:      env("ROUTEGATE_AGENT_ID", "local-dev-agent"),
		AgentToken:   env("ROUTEGATE_AGENT_TOKEN", "dev-agent-token-change-me"),
		PollInterval: time.Duration(envInt("ROUTEGATE_AGENT_POLL_INTERVAL_SECONDS", 15)) * time.Second,
		LogLevel:     parseLogLevel(env("ROUTEGATE_LOG_LEVEL", "info")),
	}
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
