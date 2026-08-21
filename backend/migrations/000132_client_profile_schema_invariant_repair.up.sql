-- Reassert the physical schema invariants required by client-profile upserts.
--
-- Historical production-like hosts may report an up-to-date migration version
-- while still missing a constraint/column if an older copy of a migration was
-- applied before the repair was introduced. This migration is deliberately
-- idempotent and uses a new version so those hosts execute the repair again.

ALTER TABLE vpn_client_profiles
  ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'auto';

UPDATE vpn_client_profiles
SET protocol = 'auto'
WHERE protocol IS NULL
   OR protocol NOT IN ('auto', 'vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto');

ALTER TABLE vpn_client_profiles
  DROP CONSTRAINT IF EXISTS vpn_client_profiles_protocol_check;

ALTER TABLE vpn_client_profiles
  ADD CONSTRAINT vpn_client_profiles_protocol_check
  CHECK (protocol IN ('auto', 'vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'));

WITH ranked_profiles AS (
  SELECT
    id,
    ROW_NUMBER() OVER (
      PARTITION BY vpn_account_id
      ORDER BY updated_at DESC, created_at DESC, id DESC
    ) AS row_number
  FROM vpn_client_profiles
)
DELETE FROM vpn_client_profiles AS profile
USING ranked_profiles AS ranked
WHERE profile.id = ranked.id
  AND ranked.row_number > 1;

DO $$
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
