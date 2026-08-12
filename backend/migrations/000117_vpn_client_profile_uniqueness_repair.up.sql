-- Repair historical schema drift in vpn_client_profiles.
--
-- GetOrCreateClientProfile relies on ON CONFLICT (vpn_account_id), so every
-- installation must enforce uniqueness for that column. Some historical hosts
-- created the table without the original UNIQUE constraint.

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
