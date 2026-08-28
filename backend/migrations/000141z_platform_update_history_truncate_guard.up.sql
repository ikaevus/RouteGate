-- Platform-update rollout/update rows are durable security and audit history.
-- Row-level DELETE guards do not fire for TRUNCATE, so protect every table whose
-- identity/history is relied on by replay prevention and immutable rollout
-- snapshots at the statement boundary as well. This repair follows the durable
-- snapshot migration and intentionally precedes 000142, which remains the latest
-- schema generation for this E3d slice.
CREATE OR REPLACE FUNCTION reject_platform_update_history_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'platform update durable history cannot be truncated';
END;
$$;

CREATE TRIGGER trg_agent_platform_update_jobs_no_truncate
BEFORE TRUNCATE ON agent_platform_update_jobs
FOR EACH STATEMENT
EXECUTE FUNCTION reject_platform_update_history_truncate();

CREATE TRIGGER trg_platform_update_rollouts_no_truncate
BEFORE TRUNCATE ON platform_update_rollouts
FOR EACH STATEMENT
EXECUTE FUNCTION reject_platform_update_history_truncate();

CREATE TRIGGER trg_platform_update_rollout_entries_no_truncate
BEFORE TRUNCATE ON platform_update_rollout_entries
FOR EACH STATEMENT
EXECUTE FUNCTION reject_platform_update_history_truncate();
