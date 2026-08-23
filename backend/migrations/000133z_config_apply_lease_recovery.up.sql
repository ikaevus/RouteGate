-- Recover config applies whose execution completed or was interrupted while the
-- Agent could not deliver the final result to Manager.
--
-- A claimed config_apply_job is an execution lease, not a permanent ownership
-- assignment. Agent heartbeats are emitted before new work is claimed and
-- config tasks are processed sequentially. If a claimed apply is still
-- in_progress after 90 seconds, the previous execution is no longer considered
-- authoritative: replay the exact rendered config. Config apply itself is
-- idempotent at the runtime boundary (stage -> validate -> atomic promote ->
-- restart -> healthcheck), so replay is safer than permanently losing desired
-- protocol state because one completion HTTP request was interrupted.
--
-- Retries remain bounded by the original job age. After 15 minutes the job
-- becomes terminally failed instead of cycling forever.

CREATE OR REPLACE FUNCTION routegate_reconcile_config_apply_lease()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
  UPDATE config_apply_jobs
  SET
    status = CASE
      WHEN created_at <= now() - interval '15 minutes' THEN 'failed'
      ELSE 'pending'
    END,
    agent_id = CASE
      WHEN created_at <= now() - interval '15 minutes' THEN agent_id
      ELSE NULL
    END,
    started_at = CASE
      WHEN created_at <= now() - interval '15 minutes' THEN started_at
      ELSE NULL
    END,
    completed_at = CASE
      WHEN created_at <= now() - interval '15 minutes' THEN COALESCE(completed_at, now())
      ELSE NULL
    END,
    result_payload = CASE
      WHEN created_at <= now() - interval '15 minutes' THEN result_payload
      ELSE '{}'::jsonb
    END,
    error_message = CASE
      WHEN created_at <= now() - interval '15 minutes'
        THEN COALESCE(
          NULLIF(error_message, ''),
          'Agent task completion was not confirmed within the config apply recovery window.'
        )
      ELSE NULL
    END,
    updated_at = now()
  WHERE agent_id = NEW.id
    AND status = 'in_progress'
    AND started_at IS NOT NULL
    AND started_at < now() - interval '90 seconds';

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS agents_reconcile_config_apply_lease ON agents;
CREATE TRIGGER agents_reconcile_config_apply_lease
AFTER UPDATE OF last_seen_at ON agents
FOR EACH ROW
WHEN (NEW.last_seen_at IS DISTINCT FROM OLD.last_seen_at)
EXECUTE FUNCTION routegate_reconcile_config_apply_lease();

-- One-time repair for installations that already hit the previous orphan-job
-- safeguard. Only the latest rendered config for each server is eligible, only
-- the exact historical completion-unconfirmed failure is accepted, and a server
-- with another active apply is left untouched.
WITH latest_versions AS (
  SELECT DISTINCT ON (server_id)
    id,
    server_id
  FROM config_versions
  ORDER BY server_id, created_at DESC, id DESC
), recoverable AS (
  SELECT j.id
  FROM config_apply_jobs j
  JOIN latest_versions v
    ON v.id = j.config_version_id
   AND v.server_id = j.server_id
  WHERE j.action = 'apply'
    AND j.status = 'failed'
    AND j.error_message = 'Agent task completion was not confirmed before a later heartbeat.'
    AND NOT EXISTS (
      SELECT 1
      FROM config_apply_jobs active_job
      WHERE active_job.server_id = j.server_id
        AND active_job.id <> j.id
        AND active_job.status IN ('pending', 'in_progress')
    )
)
UPDATE config_apply_jobs j
SET
  status = 'pending',
  agent_id = NULL,
  started_at = NULL,
  completed_at = NULL,
  result_payload = '{}'::jsonb,
  error_message = NULL,
  updated_at = now()
FROM recoverable r
WHERE j.id = r.id;
