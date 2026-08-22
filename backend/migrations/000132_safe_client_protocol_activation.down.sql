CREATE OR REPLACE FUNCTION routegate_mark_config_version_applied()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.action = 'apply' AND NEW.status = 'succeeded' THEN
    UPDATE config_versions
    SET
      status = 'applied',
      applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
    WHERE id = NEW.config_version_id;
  END IF;

  RETURN NEW;
END;
$$;

ALTER TABLE vpn_client_profiles
    DROP CONSTRAINT IF EXISTS vpn_client_profiles_active_protocol_check;

ALTER TABLE vpn_client_profiles
    DROP COLUMN IF EXISTS active_protocol;
