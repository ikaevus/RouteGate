package db

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
	"github.com/ikaevus/routegate/backend/internal/delivery"
	"github.com/ikaevus/routegate/backend/internal/secrets"
)

func TestManagedTelegramSettingsEncryptSecretAndApplyWithoutRestart(t *testing.T) {
	databaseURL := os.Getenv("ROUTEGATE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ROUTEGATE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := Connect(ctx, databaseURL, logger)
	if err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	defer pool.Close()

	resetPublicSchema(t, ctx, pool)
	if err := Migrate(ctx, pool, "../../migrations", logger); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "master.key")
	cfg := config.Config{
		SecretsKeyFile: keyPath,
		Telegram:       config.TelegramConfig{BotToken: "legacy-token"},
	}
	if err := delivery.EnsureProviderSecretStore(ctx, pool, cfg, logger); err != nil {
		t.Fatalf("initialize secret store: %v", err)
	}
	manager := delivery.NewProviderSettingsManager(pool, cfg, logger)
	managedSecret := "123456789:managed-super-secret-token"

	view, err := manager.Save(ctx, "telegram", delivery.ProviderSettingsRequest{
		Config: []byte(`{}`),
		Secret: &managedSecret,
	}, "")
	if err != nil {
		t.Fatalf("save managed Telegram settings: %v", err)
	}
	if view.Source != "managed" || !view.Ready || !view.SecretConfigured {
		t.Fatalf("unexpected managed settings view: %+v", view)
	}

	var ciphertext, nonce, configJSON []byte
	var keyVersion int
	if err := pool.QueryRow(ctx, `
		SELECT secret_ciphertext, secret_nonce, secret_key_version, config_json
		FROM delivery_provider_settings
		WHERE provider='telegram'
	`).Scan(&ciphertext, &nonce, &keyVersion, &configJSON); err != nil {
		t.Fatalf("read stored managed settings: %v", err)
	}
	if len(ciphertext) == 0 || len(nonce) == 0 {
		t.Fatal("managed secret ciphertext or nonce is empty")
	}
	if bytes.Contains(ciphertext, []byte(managedSecret)) || bytes.Contains(configJSON, []byte(managedSecret)) {
		t.Fatal("plaintext provider secret persisted in PostgreSQL")
	}

	box, err := secrets.LoadBox(keyPath)
	if err != nil {
		t.Fatalf("load generated master key: %v", err)
	}
	plaintext, err := box.Open(ciphertext, nonce, []byte("routegate:delivery-provider-settings:telegram:v1"))
	if err != nil {
		t.Fatalf("decrypt stored provider settings: %v", err)
	}
	if !bytes.Contains(plaintext, []byte(managedSecret)) || bytes.Contains(plaintext, []byte("legacy-token")) {
		t.Fatalf("unexpected decrypted managed credential envelope: %s", plaintext)
	}
	if keyVersion != secrets.CurrentVersion {
		t.Fatalf("secret key version = %d, want %d", keyVersion, secrets.CurrentVersion)
	}

	resolved, ok, err := manager.Resolve(ctx, "telegram")
	if err != nil || !ok {
		t.Fatalf("resolve managed Telegram provider: ok=%v err=%v", ok, err)
	}
	configurable, ok := resolved.(interface{ Configured() bool })
	if !ok || !configurable.Configured() {
		t.Fatalf("managed provider was not ready immediately after save: %T", resolved)
	}

	// A browser never receives the stored secret, so saving again with the
	// secret omitted must retain and re-encrypt the existing credential.
	if _, err := manager.Save(ctx, "telegram", delivery.ProviderSettingsRequest{Config: []byte(`{}`)}, ""); err != nil {
		t.Fatalf("save managed settings while retaining existing secret: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT secret_ciphertext, secret_nonce
		FROM delivery_provider_settings
		WHERE provider='telegram'
	`).Scan(&ciphertext, &nonce); err != nil {
		t.Fatalf("read retained encrypted secret: %v", err)
	}
	plaintext, err = box.Open(ciphertext, nonce, []byte("routegate:delivery-provider-settings:telegram:v1"))
	if err != nil || !bytes.Contains(plaintext, []byte(managedSecret)) {
		t.Fatalf("metadata-only save lost the managed credential: err=%v payload=%s", err, plaintext)
	}

	if err := manager.Delete(ctx, "telegram"); err != nil {
		t.Fatalf("remove managed Telegram settings: %v", err)
	}
	fallback, err := manager.View(ctx, "telegram")
	if err != nil {
		t.Fatalf("read legacy fallback after managed settings removal: %v", err)
	}
	if fallback.Source != "environment" || !fallback.SecretConfigured {
		t.Fatalf("legacy environment fallback was not restored: %+v", fallback)
	}
}
