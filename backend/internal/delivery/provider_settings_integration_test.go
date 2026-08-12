package delivery

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/config"
	"github.com/ikaevus/routegate/backend/internal/db"
)

func TestManagedTelegramSettingsEncryptSecretAndApplyWithoutRestart(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}
	if err := db.Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	cfg := config.Config{
		SecretsKeyFile: filepath.Join(t.TempDir(), "master.key"),
		Telegram:       config.TelegramConfig{BotToken: "legacy-token"},
	}
	if err := EnsureProviderSecretStore(ctx, pool, cfg, logger); err != nil {
		t.Fatalf("initialize secret store: %v", err)
	}
	manager := NewProviderSettingsManager(pool, cfg, logger)
	secret := "123456789:managed-super-secret-token"

	view, err := manager.Save(ctx, "telegram", ProviderSettingsRequest{
		Config: []byte(`{}`),
		Secret: &secret,
	}, "")
	if err != nil {
		t.Fatalf("save managed Telegram settings: %v", err)
	}
	if view.Source != providerSourceManaged || !view.Ready || !view.SecretConfigured {
		t.Fatalf("unexpected managed settings view: %+v", view)
	}

	var ciphertext []byte
	var configJSON []byte
	if err := pool.QueryRow(ctx, `
		SELECT secret_ciphertext, config_json
		FROM delivery_provider_settings
		WHERE provider='telegram'
	`).Scan(&ciphertext, &configJSON); err != nil {
		t.Fatalf("read stored managed settings: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("managed secret ciphertext is empty")
	}
	if bytes.Contains(ciphertext, []byte(secret)) || bytes.Contains(configJSON, []byte(secret)) {
		t.Fatal("plaintext provider secret persisted in PostgreSQL")
	}

	resolved, ok, err := manager.Resolve(ctx, "telegram")
	if err != nil || !ok {
		t.Fatalf("resolve managed Telegram provider: ok=%v err=%v", ok, err)
	}
	telegram, ok := resolved.(*TelegramProvider)
	if !ok {
		t.Fatalf("resolved provider type = %T, want *TelegramProvider", resolved)
	}
	if telegram.botToken != secret {
		t.Fatal("runtime provider did not receive the managed credential")
	}

	// Saving again without a secret must retain the encrypted credential rather
	// than requiring it to be returned to the browser and posted back.
	if _, err := manager.Save(ctx, "telegram", ProviderSettingsRequest{Config: []byte(`{}`)}, ""); err != nil {
		t.Fatalf("save managed settings while retaining existing secret: %v", err)
	}
	resolved, ok, err = manager.Resolve(ctx, "telegram")
	if err != nil || !ok {
		t.Fatalf("resolve Telegram provider after metadata-only save: ok=%v err=%v", ok, err)
	}
	telegram = resolved.(*TelegramProvider)
	if telegram.botToken != secret {
		t.Fatal("metadata-only save replaced or lost the stored credential")
	}

	if err := manager.Delete(ctx, "telegram"); err != nil {
		t.Fatalf("remove managed Telegram settings: %v", err)
	}
	fallback, err := manager.View(ctx, "telegram")
	if err != nil {
		t.Fatalf("read legacy fallback after managed settings removal: %v", err)
	}
	if fallback.Source != providerSourceEnvironment || !fallback.SecretConfigured {
		t.Fatalf("legacy environment fallback was not restored: %+v", fallback)
	}
}
