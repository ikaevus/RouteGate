CREATE TABLE deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vpn_account_id UUID REFERENCES vpn_accounts(id) ON DELETE SET NULL,
    channel TEXT NOT NULL,
    provider TEXT NOT NULL,
    recipient TEXT NOT NULL,
    template_key TEXT NOT NULL,
    locale TEXT NOT NULL DEFAULT 'en',
    attach_qr BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ DEFAULT now(),
    attempt_started_at TIMESTAMPTZ,
    provider_reference TEXT,
    last_error_class TEXT,
    last_error_code TEXT,
    idempotency_key TEXT,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT deliveries_channel_not_blank CHECK (btrim(channel) <> ''),
    CONSTRAINT deliveries_provider_not_blank CHECK (btrim(provider) <> ''),
    CONSTRAINT deliveries_recipient_not_blank CHECK (btrim(recipient) <> ''),
    CONSTRAINT deliveries_template_key_not_blank CHECK (btrim(template_key) <> ''),
    CONSTRAINT deliveries_locale_check CHECK (locale IN ('en', 'ru')),
    CONSTRAINT deliveries_status_check CHECK (
        status IN ('queued', 'sending', 'retrying', 'sent', 'delivered', 'failed', 'uncertain')
    ),
    CONSTRAINT deliveries_attempt_count_check CHECK (attempt_count >= 0),
    CONSTRAINT deliveries_max_attempts_check CHECK (max_attempts BETWEEN 1 AND 20),
    CONSTRAINT deliveries_attempt_bounds_check CHECK (attempt_count <= max_attempts)
);

CREATE INDEX idx_deliveries_claim
    ON deliveries (next_attempt_at, created_at, id)
    WHERE status IN ('queued', 'retrying');

CREATE INDEX idx_deliveries_vpn_account_history
    ON deliveries (vpn_account_id, created_at DESC, id DESC)
    WHERE vpn_account_id IS NOT NULL;

CREATE UNIQUE INDEX idx_deliveries_idempotency_key
    ON deliveries (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
