CREATE TABLE delivery_recipients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel TEXT NOT NULL,
    provider TEXT NOT NULL,
    address TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT delivery_recipients_channel_check
        CHECK (channel IN ('email', 'telegram', 'whatsapp')),
    CONSTRAINT delivery_recipients_provider_check
        CHECK (provider IN ('smtp', 'telegram', 'whatsapp')),
    CONSTRAINT delivery_recipients_provider_channel_check
        CHECK (
            (provider = 'smtp' AND channel = 'email') OR
            (provider = 'telegram' AND channel = 'telegram') OR
            (provider = 'whatsapp' AND channel = 'whatsapp')
        ),
    CONSTRAINT delivery_recipients_provider_address_unique UNIQUE (provider, address)
);

CREATE INDEX idx_delivery_recipients_channel_enabled
    ON delivery_recipients(channel, enabled, display_name, created_at);

CREATE TABLE telegram_pairing_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    start_parameter_hash BYTEA NOT NULL UNIQUE,
    bot_username TEXT NOT NULL,
    recipient_id UUID REFERENCES delivery_recipients(id) ON DELETE SET NULL,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT telegram_pairing_sessions_hash_length_check
        CHECK (octet_length(start_parameter_hash) = 32),
    CONSTRAINT telegram_pairing_sessions_expiry_check
        CHECK (expires_at > created_at)
);

CREATE INDEX idx_telegram_pairing_sessions_pending
    ON telegram_pairing_sessions(expires_at, created_at)
    WHERE recipient_id IS NULL;

CREATE TABLE telegram_update_state (
    provider TEXT PRIMARY KEY,
    next_update_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT telegram_update_state_provider_check CHECK (provider = 'telegram'),
    CONSTRAINT telegram_update_state_offset_check CHECK (next_update_id >= 0)
);

INSERT INTO telegram_update_state(provider, next_update_id)
VALUES ('telegram', 0)
ON CONFLICT (provider) DO NOTHING;
