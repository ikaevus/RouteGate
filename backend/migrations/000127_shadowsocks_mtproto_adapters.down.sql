DROP INDEX IF EXISTS idx_vpn_accounts_shadowsocks_user_key;
DROP INDEX IF EXISTS idx_servers_mtproto_secret;
DROP INDEX IF EXISTS idx_servers_shadowsocks_server_key;

ALTER TABLE vpn_accounts
    DROP CONSTRAINT IF EXISTS vpn_accounts_shadowsocks_user_key_check;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_mtproto_secret_check,
    DROP CONSTRAINT IF EXISTS servers_mtproto_fronting_domain_check,
    DROP CONSTRAINT IF EXISTS servers_mtproto_port_check,
    DROP CONSTRAINT IF EXISTS servers_shadowsocks_server_key_check,
    DROP CONSTRAINT IF EXISTS servers_shadowsocks_method_check,
    DROP CONSTRAINT IF EXISTS servers_shadowsocks_port_check,
    DROP CONSTRAINT IF EXISTS servers_vpn_protocol_check;

ALTER TABLE servers
    ADD CONSTRAINT servers_vpn_protocol_check
    CHECK (vpn_protocol IN ('vless', 'wireguard', 'hysteria2'));

CREATE OR REPLACE FUNCTION routegate_mark_vpn_account_servers_dirty_after_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    WITH changed_servers AS (
        SELECT o.server_id
        FROM old_rows o
        JOIN new_rows n USING (id)
        WHERE (
            o.display_name IS DISTINCT FROM n.display_name
            OR o.status IS DISTINCT FROM n.status
            OR o.server_id IS DISTINCT FROM n.server_id
            OR o.vless_uuid IS DISTINCT FROM n.vless_uuid
            OR o.hysteria2_password IS DISTINCT FROM n.hysteria2_password
        )
          AND o.server_id IS NOT NULL

        UNION

        SELECT n.server_id
        FROM old_rows o
        JOIN new_rows n USING (id)
        WHERE (
            o.display_name IS DISTINCT FROM n.display_name
            OR o.status IS DISTINCT FROM n.status
            OR o.server_id IS DISTINCT FROM n.server_id
            OR o.vless_uuid IS DISTINCT FROM n.vless_uuid
            OR o.hysteria2_password IS DISTINCT FROM n.hysteria2_password
        )
          AND n.server_id IS NOT NULL
    )
    UPDATE servers s
    SET vpn_accounts_config_updated_at = now()
    WHERE s.id IN (SELECT server_id FROM changed_servers);

    RETURN NULL;
END;
$$;

ALTER TABLE vpn_accounts
    DROP COLUMN IF EXISTS shadowsocks_user_key;

ALTER TABLE servers
    DROP COLUMN IF EXISTS mtproto_fronting_domain,
    DROP COLUMN IF EXISTS mtproto_secret,
    DROP COLUMN IF EXISTS mtproto_port,
    DROP COLUMN IF EXISTS shadowsocks_server_key,
    DROP COLUMN IF EXISTS shadowsocks_method,
    DROP COLUMN IF EXISTS shadowsocks_port;
