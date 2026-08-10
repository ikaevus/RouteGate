CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS config_updated_at TIMESTAMPTZ;

UPDATE vpn_accounts
SET config_updated_at = updated_at
WHERE config_updated_at IS NULL;

ALTER TABLE vpn_accounts
    ALTER COLUMN config_updated_at SET DEFAULT now(),
    ALTER COLUMN config_updated_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_display_name_trgm
    ON vpn_accounts USING gin (display_name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_email_trgm
    ON vpn_accounts USING gin (email gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_vless_uuid
    ON vpn_accounts(vless_uuid);

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_created_at_id
    ON vpn_accounts(created_at DESC, id DESC);
