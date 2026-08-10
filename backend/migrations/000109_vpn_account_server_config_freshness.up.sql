ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS vpn_accounts_config_updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Existing installations may already contain an applied configuration that still
-- references an account which has since been unassigned or moved. There is no
-- reliable way to reconstruct that old server relationship from the current
-- vpn_accounts row, so the migration intentionally marks every existing server
-- dirty once. A fresh render/apply establishes the new baseline.
UPDATE servers
SET vpn_accounts_config_updated_at = now();

CREATE OR REPLACE FUNCTION routegate_mark_vpn_account_servers_dirty_after_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE servers s
    SET vpn_accounts_config_updated_at = now()
    WHERE s.id IN (
        SELECT DISTINCT n.server_id
        FROM new_rows n
        WHERE n.server_id IS NOT NULL
    );

    RETURN NULL;
END;
$$;

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

CREATE OR REPLACE FUNCTION routegate_mark_vpn_account_servers_dirty_after_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE servers s
    SET vpn_accounts_config_updated_at = now()
    WHERE s.id IN (
        SELECT DISTINCT o.server_id
        FROM old_rows o
        WHERE o.server_id IS NOT NULL
    );

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_vpn_accounts_mark_servers_dirty_after_insert ON vpn_accounts;
CREATE TRIGGER trg_vpn_accounts_mark_servers_dirty_after_insert
AFTER INSERT ON vpn_accounts
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT
EXECUTE FUNCTION routegate_mark_vpn_account_servers_dirty_after_insert();

DROP TRIGGER IF EXISTS trg_vpn_accounts_mark_servers_dirty_after_update ON vpn_accounts;
CREATE TRIGGER trg_vpn_accounts_mark_servers_dirty_after_update
AFTER UPDATE ON vpn_accounts
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows
FOR EACH STATEMENT
EXECUTE FUNCTION routegate_mark_vpn_account_servers_dirty_after_update();

DROP TRIGGER IF EXISTS trg_vpn_accounts_mark_servers_dirty_after_delete ON vpn_accounts;
CREATE TRIGGER trg_vpn_accounts_mark_servers_dirty_after_delete
AFTER DELETE ON vpn_accounts
REFERENCING OLD TABLE AS old_rows
FOR EACH STATEMENT
EXECUTE FUNCTION routegate_mark_vpn_account_servers_dirty_after_delete();
