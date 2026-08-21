CREATE TABLE vpn_account_automatic_selection_policies (
    vpn_account_id UUID PRIMARY KEY REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    allow_degraded BOOLEAN NOT NULL DEFAULT FALSE,
    cooldown_seconds INTEGER NOT NULL DEFAULT 300,
    last_selected_at TIMESTAMPTZ,
    last_selected_server_id UUID REFERENCES servers(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT vpn_account_automatic_selection_cooldown_check
        CHECK (cooldown_seconds BETWEEN 60 AND 86400)
);

CREATE INDEX idx_vpn_account_automatic_selection_enabled
    ON vpn_account_automatic_selection_policies(enabled)
    WHERE enabled = TRUE;
