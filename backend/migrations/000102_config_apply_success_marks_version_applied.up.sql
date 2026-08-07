-- Keep config_versions in sync with the authoritative Agent apply result.
-- A successful config_apply_jobs row means the matching rendered config is the
-- version currently applied on the server.

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

-- Repair successful applies recorded before this invariant existed. Prefer the
-- Agent-confirmed completion timestamp so historical state remains truthful.
UPDATE config_versions cv
SET
  status = 'applied',
  applied_at = COALESCE(cv.applied_at, j.completed_at, j.updated_at, now())
FROM LATERAL (
  SELECT completed_at, updated_at
  FROM config_apply_jobs j
  WHERE j.config_version_id = cv.id
    AND j.action = 'apply'
    AND j.status = 'succeeded'
  ORDER BY COALESCE(j.completed_at, j.updated_at) DESC
  LIMIT 1
) j
WHERE cv.status <> 'applied' OR cv.applied_at IS NULL;
