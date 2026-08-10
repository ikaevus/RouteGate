DROP TRIGGER IF EXISTS trg_vpn_accounts_mark_servers_dirty_after_delete ON vpn_accounts;
DROP TRIGGER IF EXISTS trg_vpn_accounts_mark_servers_dirty_after_update ON vpn_accounts;
DROP TRIGGER IF EXISTS trg_vpn_accounts_mark_servers_dirty_after_insert ON vpn_accounts;

DROP FUNCTION IF EXISTS routegate_mark_vpn_account_servers_dirty_after_delete();
DROP FUNCTION IF EXISTS routegate_mark_vpn_account_servers_dirty_after_update();
DROP FUNCTION IF EXISTS routegate_mark_vpn_account_servers_dirty_after_insert();

ALTER TABLE servers
    DROP COLUMN IF EXISTS vpn_accounts_config_updated_at;
