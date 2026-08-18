DROP INDEX IF EXISTS idx_vpn_accounts_hysteria2_password;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_hysteria2_port_check,
    DROP CONSTRAINT IF EXISTS servers_vpn_protocol_check;

ALTER TABLE servers
    ADD CONSTRAINT servers_vpn_protocol_check
    CHECK (vpn_protocol IN ('vless', 'wireguard'));

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
    DROP COLUMN IF EXISTS hysteria2_password;

ALTER TABLE servers
    DROP COLUMN IF EXISTS hysteria2_masquerade_url,
    DROP COLUMN IF EXISTS hysteria2_acme_email,
    DROP COLUMN IF EXISTS hysteria2_domain,
    DROP COLUMN IF EXISTS hysteria2_port;
