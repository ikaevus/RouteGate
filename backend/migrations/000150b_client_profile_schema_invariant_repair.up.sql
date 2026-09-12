-- Repair canonical vpn_client_profiles invariants that can be lost by a
-- partially destructive restore while schema_migrations still records the
-- historical repair migrations as applied.
--
-- This migration intentionally sorts after 000150/000150a and before 000151 so
-- production-like hosts with physical schema drift are repaired before the
-- RG-116 delivery migration and deploy validation run.

DO $$
DECLARE
  duplicate_account UUID;
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_index AS index_row
    JOIN pg_attribute AS attribute_row
      ON attribute_row.attrelid = index_row.indrelid
     AND attribute_row.attnum = index_row.indkey[0]
    WHERE index_row.indrelid = 'vpn_client_profiles'::regclass
      AND index_row.indisunique
      AND index_row.indisvalid
      AND index_row.indpred IS NULL
      AND index_row.indexprs IS NULL
      AND index_row.indnkeyatts = 1
      AND attribute_row.attname = 'vpn_account_id'
  ) THEN
    SELECT vpn_account_id
    INTO duplicate_account
    FROM vpn_client_profiles
    GROUP BY vpn_account_id
    HAVING COUNT(*) > 1
    LIMIT 1;

    IF duplicate_account IS NOT NULL THEN
      RAISE EXCEPTION
        'cannot repair vpn_client_profiles uniqueness: duplicate vpn_account_id %',
        duplicate_account;
    END IF;

    ALTER TABLE vpn_client_profiles
      ADD CONSTRAINT vpn_client_profiles_vpn_account_id_key
      UNIQUE (vpn_account_id);
  END IF;
END $$;

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
