DROP INDEX IF EXISTS idx_vpn_accounts_vless_uuid;

ALTER TABLE vpn_accounts
    DROP CONSTRAINT IF EXISTS vpn_accounts_vless_uuid_unique;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_vless_network_check,
    DROP CONSTRAINT IF EXISTS servers_vless_flow_check,
    DROP CONSTRAINT IF EXISTS servers_vless_port_check,
    DROP COLUMN IF EXISTS reality_server_name,
    DROP COLUMN IF EXISTS reality_short_id,
    DROP COLUMN IF EXISTS reality_public_key,
    DROP COLUMN IF EXISTS vless_network,
    DROP COLUMN IF EXISTS vless_flow,
    DROP COLUMN IF EXISTS vless_port;

ALTER TABLE vpn_accounts
    DROP COLUMN IF EXISTS vless_uuid;
