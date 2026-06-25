CREATE TABLE vpn_subscription_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vpn_account_id UUID NOT NULL REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT vpn_subscription_tokens_status_check CHECK (status IN ('active', 'revoked'))
);

CREATE UNIQUE INDEX idx_vpn_subscription_tokens_active_account
    ON vpn_subscription_tokens(vpn_account_id)
    WHERE status = 'active';

CREATE INDEX idx_vpn_subscription_tokens_account_id ON vpn_subscription_tokens(vpn_account_id);
CREATE INDEX idx_vpn_subscription_tokens_hash ON vpn_subscription_tokens(token_hash);
CREATE INDEX idx_vpn_subscription_tokens_status ON vpn_subscription_tokens(status);
