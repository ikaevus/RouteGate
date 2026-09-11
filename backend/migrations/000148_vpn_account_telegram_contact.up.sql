ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS phone TEXT,
    ADD COLUMN IF NOT EXISTS telegram_username TEXT,
    ADD COLUMN IF NOT EXISTS telegram_recipient_id UUID REFERENCES delivery_recipients(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_telegram_recipient_id
    ON vpn_accounts(telegram_recipient_id)
    WHERE telegram_recipient_id IS NOT NULL;

-- Lets a Telegram pairing invite started from a specific VPN account link the
-- resulting recipient back to that account once the person presses Start.
ALTER TABLE telegram_pairing_sessions
    ADD COLUMN IF NOT EXISTS vpn_account_id UUID REFERENCES vpn_accounts(id) ON DELETE SET NULL;
