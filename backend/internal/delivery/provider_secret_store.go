package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/secrets"
)

func EnsureProviderSecretStore(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, logger *slog.Logger) error {
	if _, err := secrets.LoadBox(cfg.SecretsKeyFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load RouteGate master key: %w", err)
	}

	var encryptedRows int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM delivery_provider_settings
		WHERE secret_ciphertext IS NOT NULL
	`).Scan(&encryptedRows); err != nil {
		return fmt.Errorf("check encrypted provider settings before master key initialization: %w", err)
	}
	if encryptedRows > 0 {
		return fmt.Errorf("RouteGate master key is missing while encrypted provider settings exist; refusing to generate a replacement key")
	}

	if err := secrets.CreateKeyFile(cfg.SecretsKeyFile); err != nil {
		if _, loadErr := secrets.LoadBox(cfg.SecretsKeyFile); loadErr == nil {
			return nil
		}
		return fmt.Errorf("initialize RouteGate master key: %w", err)
	}
	if _, err := secrets.LoadBox(cfg.SecretsKeyFile); err != nil {
		return fmt.Errorf("verify RouteGate master key: %w", err)
	}
	if logger != nil {
		logger.Info("initialized RouteGate managed secret store", "component", "secret_store")
	}
	return nil
}
