ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS display_name TEXT,
    ADD COLUMN IF NOT EXISTS email TEXT,
    ADD COLUMN IF NOT EXISTS max_devices INTEGER,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE vpn_accounts
SET display_name = COALESCE(NULLIF(display_name, ''), username)
WHERE display_name IS NULL OR display_name = '';

ALTER TABLE vpn_accounts
    ALTER COLUMN display_name SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'created';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'vpn_accounts_status_check'
    ) THEN
        ALTER TABLE vpn_accounts
            ADD CONSTRAINT vpn_accounts_status_check
            CHECK (status IN ('created', 'active', 'suspended', 'expired', 'revoked'));
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'vpn_accounts_max_devices_check'
    ) THEN
        ALTER TABLE vpn_accounts
            ADD CONSTRAINT vpn_accounts_max_devices_check
            CHECK (max_devices IS NULL OR max_devices > 0);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_status ON vpn_accounts(status);
CREATE INDEX IF NOT EXISTS idx_vpn_accounts_server_id ON vpn_accounts(server_id);
CREATE INDEX IF NOT EXISTS idx_vpn_accounts_email ON vpn_accounts(email);
