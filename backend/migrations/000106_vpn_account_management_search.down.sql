DROP INDEX IF EXISTS idx_vpn_accounts_created_at_id;
DROP INDEX IF EXISTS idx_vpn_accounts_vless_uuid;
DROP INDEX IF EXISTS idx_vpn_accounts_email_trgm;
DROP INDEX IF EXISTS idx_vpn_accounts_display_name_trgm;

ALTER TABLE vpn_accounts
    DROP COLUMN IF EXISTS config_updated_at;

-- pg_trgm is intentionally retained because extensions are database-wide
-- and may be shared by other RouteGate search features.
