CREATE OR REPLACE FUNCTION routegate_vpn_account_config_timestamp_invariant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.display_name IS DISTINCT FROM NEW.display_name
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.server_id IS DISTINCT FROM NEW.server_id THEN
        NEW.config_updated_at := now();
    ELSE
        NEW.config_updated_at := OLD.config_updated_at;
    END IF;

    IF OLD.display_name IS NOT DISTINCT FROM NEW.display_name
       AND OLD.email IS NOT DISTINCT FROM NEW.email
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.expires_at IS NOT DISTINCT FROM NEW.expires_at
       AND OLD.max_devices IS NOT DISTINCT FROM NEW.max_devices
       AND OLD.server_id IS NOT DISTINCT FROM NEW.server_id THEN
        NEW.updated_at := OLD.updated_at;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_vpn_accounts_config_timestamp_invariant ON vpn_accounts;

CREATE TRIGGER trg_vpn_accounts_config_timestamp_invariant
BEFORE UPDATE ON vpn_accounts
FOR EACH ROW
EXECUTE FUNCTION routegate_vpn_account_config_timestamp_invariant();
