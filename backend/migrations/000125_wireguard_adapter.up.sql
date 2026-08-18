ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS vpn_protocol TEXT NOT NULL DEFAULT 'vless',
    ADD COLUMN IF NOT EXISTS wireguard_port INTEGER NOT NULL DEFAULT 51820,
	ADD COLUMN IF NOT EXISTS wireguard_address INET NOT NULL DEFAULT '10.66.0.1/24',
    ADD COLUMN IF NOT EXISTS wireguard_dns INET NOT NULL DEFAULT '1.1.1.1',
    ADD COLUMN IF NOT EXISTS wireguard_private_key TEXT,
    ADD COLUMN IF NOT EXISTS wireguard_public_key TEXT;

ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS wireguard_private_key TEXT,
    ADD COLUMN IF NOT EXISTS wireguard_public_key TEXT,
    ADD COLUMN IF NOT EXISTS wireguard_address INET;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'servers_vpn_protocol_check'
    ) THEN
        ALTER TABLE servers
            ADD CONSTRAINT servers_vpn_protocol_check
            CHECK (vpn_protocol IN ('vless', 'wireguard'));
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'servers_wireguard_port_check'
    ) THEN
        ALTER TABLE servers
            ADD CONSTRAINT servers_wireguard_port_check
            CHECK (wireguard_port > 0 AND wireguard_port <= 65535);
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_accounts_wireguard_public_key
    ON vpn_accounts(wireguard_public_key)
    WHERE wireguard_public_key IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_accounts_server_wireguard_address
    ON vpn_accounts(server_id, wireguard_address)
    WHERE server_id IS NOT NULL AND wireguard_address IS NOT NULL;
