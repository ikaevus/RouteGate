ALTER TABLE telegram_pairing_sessions
    DROP COLUMN IF EXISTS vpn_account_id;

DROP INDEX IF EXISTS idx_vpn_accounts_telegram_recipient_id;

ALTER TABLE vpn_accounts
    DROP COLUMN IF EXISTS telegram_recipient_id,
    DROP COLUMN IF EXISTS telegram_username,
    DROP COLUMN IF EXISTS phone;
