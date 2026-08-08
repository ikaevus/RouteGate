DROP TRIGGER IF EXISTS config_apply_jobs_finalize_lifecycle ON config_apply_jobs;
DROP FUNCTION IF EXISTS routegate_finalize_config_apply_lifecycle();
DROP FUNCTION IF EXISTS routegate_prune_config_apply_jobs(UUID);
DROP FUNCTION IF EXISTS routegate_prune_config_versions(UUID);

DROP INDEX IF EXISTS idx_config_versions_server_pinned_applied;

ALTER TABLE config_versions
    DROP COLUMN IF EXISTS pinned;

ALTER TABLE servers
    DROP COLUMN IF EXISTS active_config_version_id;

-- Restore the pre-RG-109 invariant from migration 000102.
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

CREATE TRIGGER config_apply_jobs_mark_version_applied
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (NEW.action = 'apply' AND NEW.status = 'succeeded')
EXECUTE FUNCTION routegate_mark_config_version_applied();
