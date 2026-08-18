ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS shadowsocks_port INTEGER NOT NULL DEFAULT 8388,
    ADD COLUMN IF NOT EXISTS shadowsocks_method TEXT NOT NULL DEFAULT '2022-blake3-aes-128-gcm',
    ADD COLUMN IF NOT EXISTS shadowsocks_server_key TEXT,
    ADD COLUMN IF NOT EXISTS mtproto_port INTEGER NOT NULL DEFAULT 8443,
    ADD COLUMN IF NOT EXISTS mtproto_secret TEXT,
    ADD COLUMN IF NOT EXISTS mtproto_fronting_domain TEXT NOT NULL DEFAULT 'www.cloudflare.com';

ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS shadowsocks_user_key TEXT;

UPDATE servers
SET shadowsocks_server_key = encode(gen_random_bytes(16), 'base64')
WHERE shadowsocks_server_key IS NULL OR shadowsocks_server_key = '';

UPDATE servers
SET mtproto_secret = 'ee' || encode(gen_random_bytes(16), 'hex') || encode(convert_to('www.cloudflare.com', 'UTF8'), 'hex')
WHERE mtproto_secret IS NULL OR mtproto_secret = '';

UPDATE vpn_accounts
SET shadowsocks_user_key = encode(gen_random_bytes(16), 'base64')
WHERE shadowsocks_user_key IS NULL OR shadowsocks_user_key = '';

ALTER TABLE servers
    ALTER COLUMN shadowsocks_server_key SET NOT NULL,
    ALTER COLUMN shadowsocks_server_key SET DEFAULT encode(gen_random_bytes(16), 'base64'),
    ALTER COLUMN mtproto_secret SET NOT NULL,
    ALTER COLUMN mtproto_secret SET DEFAULT ('ee' || encode(gen_random_bytes(16), 'hex') || encode(convert_to('www.cloudflare.com', 'UTF8'), 'hex'));

ALTER TABLE vpn_accounts
    ALTER COLUMN shadowsocks_user_key SET NOT NULL,
    ALTER COLUMN shadowsocks_user_key SET DEFAULT encode(gen_random_bytes(16), 'base64');

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_vpn_protocol_check;

ALTER TABLE servers
    ADD CONSTRAINT servers_vpn_protocol_check
    CHECK (vpn_protocol IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')),
    ADD CONSTRAINT servers_shadowsocks_port_check
    CHECK (shadowsocks_port > 0 AND shadowsocks_port <= 65535),
    ADD CONSTRAINT servers_shadowsocks_method_check
    CHECK (shadowsocks_method = '2022-blake3-aes-128-gcm'),
    ADD CONSTRAINT servers_shadowsocks_server_key_check
    CHECK (shadowsocks_server_key ~ '^[A-Za-z0-9+/]{22}==$'),
    ADD CONSTRAINT servers_mtproto_port_check
    CHECK (mtproto_port > 0 AND mtproto_port <= 65535),
    ADD CONSTRAINT servers_mtproto_fronting_domain_check
    CHECK (mtproto_fronting_domain = 'www.cloudflare.com'),
    ADD CONSTRAINT servers_mtproto_secret_check
    CHECK (mtproto_secret ~ '^ee[0-9a-f]{68}$' AND right(mtproto_secret, 36) = encode(convert_to('www.cloudflare.com', 'UTF8'), 'hex'));

ALTER TABLE vpn_accounts
    ADD CONSTRAINT vpn_accounts_shadowsocks_user_key_check
    CHECK (shadowsocks_user_key ~ '^[A-Za-z0-9+/]{22}==$');

CREATE UNIQUE INDEX IF NOT EXISTS idx_servers_shadowsocks_server_key
    ON servers(shadowsocks_server_key);

CREATE UNIQUE INDEX IF NOT EXISTS idx_servers_mtproto_secret
    ON servers(mtproto_secret);

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_accounts_shadowsocks_user_key
    ON vpn_accounts(shadowsocks_user_key);

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
            OR o.shadowsocks_user_key IS DISTINCT FROM n.shadowsocks_user_key
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
            OR o.shadowsocks_user_key IS DISTINCT FROM n.shadowsocks_user_key
        )
          AND n.server_id IS NOT NULL
    )
    UPDATE servers s
    SET vpn_accounts_config_updated_at = now()
    WHERE s.id IN (SELECT server_id FROM changed_servers);

    RETURN NULL;
END;
$$;
