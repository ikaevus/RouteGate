ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS hysteria2_port INTEGER NOT NULL DEFAULT 443,
    ADD COLUMN IF NOT EXISTS hysteria2_domain TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hysteria2_acme_email TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hysteria2_masquerade_url TEXT NOT NULL DEFAULT 'https://www.cloudflare.com/';

ALTER TABLE vpn_accounts
    ADD COLUMN IF NOT EXISTS hysteria2_password TEXT;

UPDATE vpn_accounts
SET hysteria2_password = encode(gen_random_bytes(24), 'hex')
WHERE hysteria2_password IS NULL OR hysteria2_password = '';

ALTER TABLE vpn_accounts
    ALTER COLUMN hysteria2_password SET NOT NULL,
    ALTER COLUMN hysteria2_password SET DEFAULT encode(gen_random_bytes(24), 'hex');

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_vpn_protocol_check;

ALTER TABLE servers
    ADD CONSTRAINT servers_vpn_protocol_check
    CHECK (vpn_protocol IN ('vless', 'wireguard', 'hysteria2'));

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'servers_hysteria2_port_check'
    ) THEN
        ALTER TABLE servers
            ADD CONSTRAINT servers_hysteria2_port_check
            CHECK (hysteria2_port > 0 AND hysteria2_port <= 65535);
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_accounts_hysteria2_password
    ON vpn_accounts(hysteria2_password);

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
