DROP INDEX IF EXISTS idx_vpn_accounts_server_wireguard_address;
DROP INDEX IF EXISTS idx_vpn_accounts_wireguard_public_key;

ALTER TABLE vpn_accounts
    DROP COLUMN IF EXISTS wireguard_address,
    DROP COLUMN IF EXISTS wireguard_public_key,
    DROP COLUMN IF EXISTS wireguard_private_key;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_wireguard_port_check,
    DROP CONSTRAINT IF EXISTS servers_vpn_protocol_check,
    DROP COLUMN IF EXISTS wireguard_public_key,
    DROP COLUMN IF EXISTS wireguard_private_key,
    DROP COLUMN IF EXISTS wireguard_dns,
    DROP COLUMN IF EXISTS wireguard_address,
    DROP COLUMN IF EXISTS wireguard_port,
    DROP COLUMN IF EXISTS vpn_protocol;
