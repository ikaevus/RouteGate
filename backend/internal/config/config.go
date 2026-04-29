package config

import (
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	Env         string
	HTTPAddr    string
	DatabaseURL string
	LogLevel    slog.Level
}

func Load() Config {
	return Config{
		Env:         env("ROUTEGATE_ENV", "dev"),
		HTTPAddr:    env("ROUTEGATE_HTTP_ADDR", ":8080"),
		DatabaseURL: env("ROUTEGATE_DATABASE_URL", "postgres://routegate:routegate_dev_password@localhost:5432/routegate?sslmode=disable"),
		LogLevel:    parseLogLevel(env("ROUTEGATE_LOG_LEVEL", "info")),
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
