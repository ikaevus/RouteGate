ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS vless_uuid UUID;

UPDATE vpn_accounts
SET vless_uuid = gen_random_uuid()
WHERE vless_uuid IS NULL;

ALTER TABLE vpn_accounts
    ALTER COLUMN vless_uuid SET NOT NULL,
    ALTER COLUMN vless_uuid SET DEFAULT gen_random_uuid();

ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS vless_port INTEGER NOT NULL DEFAULT 443,
    ADD COLUMN IF NOT EXISTS vless_flow TEXT,
    ADD COLUMN IF NOT EXISTS vless_network TEXT NOT NULL DEFAULT 'tcp',
    ADD COLUMN IF NOT EXISTS reality_public_key TEXT,
    ADD COLUMN IF NOT EXISTS reality_short_id TEXT,
    ADD COLUMN IF NOT EXISTS reality_server_name TEXT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'vpn_accounts_vless_uuid_unique'
    ) THEN
        ALTER TABLE vpn_accounts
            ADD CONSTRAINT vpn_accounts_vless_uuid_unique UNIQUE (vless_uuid);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'servers_vless_port_check'
    ) THEN
        ALTER TABLE servers
            ADD CONSTRAINT servers_vless_port_check
            CHECK (vless_port > 0 AND vless_port <= 65535);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'servers_vless_flow_check'
    ) THEN
        ALTER TABLE servers
            ADD CONSTRAINT servers_vless_flow_check
            CHECK (vless_flow IS NULL OR vless_flow = 'xtls-rprx-vision');
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'servers_vless_network_check'
    ) THEN
        ALTER TABLE servers
            ADD CONSTRAINT servers_vless_network_check
            CHECK (vless_network IN ('tcp', 'udp'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_vpn_accounts_vless_uuid ON vpn_accounts(vless_uuid);
