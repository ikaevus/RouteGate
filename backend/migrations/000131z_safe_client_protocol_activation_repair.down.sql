-- Keep rollback safe: restore the pre-safe-activation config lifecycle trigger.

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

DROP TRIGGER IF EXISTS config_apply_jobs_mark_version_applied ON config_apply_jobs;

CREATE TRIGGER config_apply_jobs_mark_version_applied
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (NEW.action = 'apply' AND NEW.status = 'succeeded')
EXECUTE FUNCTION routegate_mark_config_version_applied();
