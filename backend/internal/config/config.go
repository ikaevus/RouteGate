package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env                       string
	HTTPAddr                  string
	DatabaseURL               string
	LogLevel                  slog.Level
	AuthSessionTTL            time.Duration
	BootstrapAdminEmail       string
	BootstrapAdminUsername    string
	BootstrapAdminPassword    string
	BootstrapAdminDisplayName string
}

func Load() Config {
	return Config{
		Env:                       env("ROUTEGATE_ENV", "dev"),
		HTTPAddr:                  env("ROUTEGATE_HTTP_ADDR", ":8080"),
		DatabaseURL:               env("ROUTEGATE_DATABASE_URL", "postgres://routegate:routegate_dev_password@localhost:5432/routegate?sslmode=disable"),
		LogLevel:                  parseLogLevel(env("ROUTEGATE_LOG_LEVEL", "info")),
		AuthSessionTTL:            time.Duration(envInt("ROUTEGATE_AUTH_SESSION_TTL_HOURS", 24)) * time.Hour,
		BootstrapAdminEmail:       env("ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminUsername:    env("ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME", ""),
		BootstrapAdminPassword:    env("ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD", ""),
		BootstrapAdminDisplayName: env("ROUTEGATE_BOOTSTRAP_ADMIN_DISPLAY_NAME", ""),
	}
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
