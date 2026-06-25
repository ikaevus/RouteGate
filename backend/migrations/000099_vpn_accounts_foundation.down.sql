DROP INDEX IF EXISTS idx_vpn_accounts_email;
DROP INDEX IF EXISTS idx_vpn_accounts_server_id;
DROP INDEX IF EXISTS idx_vpn_accounts_status;

ALTER TABLE vpn_accounts
    DROP CONSTRAINT IF EXISTS vpn_accounts_max_devices_check,
    DROP CONSTRAINT IF EXISTS vpn_accounts_status_check;

ALTER TABLE vpn_accounts
    ALTER COLUMN status SET DEFAULT 'active',
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS max_devices,
    DROP COLUMN IF EXISTS email,
    DROP COLUMN IF EXISTS display_name;
