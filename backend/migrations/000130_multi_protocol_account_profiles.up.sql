ALTER TABLE vpn_client_profiles
    ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'auto';

ALTER TABLE vpn_client_profiles
    DROP CONSTRAINT IF EXISTS vpn_client_profiles_protocol_check;

ALTER TABLE vpn_client_profiles
    ADD CONSTRAINT vpn_client_profiles_protocol_check
    CHECK (protocol IN ('auto', 'vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'));

-- Changing a client protocol changes which server runtime must contain the
-- account. Mark the assigned node dirty so Guided Workflow can lead the
-- administrator to render/deploy the updated configuration.
CREATE OR REPLACE FUNCTION routegate_mark_vpn_client_profile_server_dirty_after_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.protocol IS DISTINCT FROM NEW.protocol THEN
        UPDATE servers s
        SET vpn_accounts_config_updated_at = now()
        FROM vpn_accounts a
        WHERE a.id = NEW.vpn_account_id
          AND a.server_id = s.id;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_vpn_client_profiles_mark_server_dirty ON vpn_client_profiles;
CREATE TRIGGER trg_vpn_client_profiles_mark_server_dirty
AFTER UPDATE OF protocol ON vpn_client_profiles
FOR EACH ROW
EXECUTE FUNCTION routegate_mark_vpn_client_profile_server_dirty_after_update();
