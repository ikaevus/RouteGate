CREATE TABLE delivery_provider_settings (
    provider TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    secret_ciphertext BYTEA,
    secret_nonce BYTEA,
    secret_key_version INTEGER NOT NULL DEFAULT 1,
    updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT delivery_provider_settings_provider_check
        CHECK (provider IN ('smtp', 'telegram', 'whatsapp')),
    CONSTRAINT delivery_provider_settings_secret_pair_check
        CHECK ((secret_ciphertext IS NULL) = (secret_nonce IS NULL)),
    CONSTRAINT delivery_provider_settings_key_version_check
        CHECK (secret_key_version > 0)
);

CREATE INDEX idx_delivery_provider_settings_updated_at
    ON delivery_provider_settings(updated_at DESC);
