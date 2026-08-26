DROP TRIGGER IF EXISTS trg_platform_update_rollout_entries_transition ON platform_update_rollout_entries;
DROP FUNCTION IF EXISTS enforce_platform_update_rollout_entry_transition();
DROP TRIGGER IF EXISTS trg_platform_update_rollouts_transition ON platform_update_rollouts;
DROP FUNCTION IF EXISTS enforce_platform_update_rollout_transition();
DROP TABLE IF EXISTS platform_update_rollout_entries;
DROP TABLE IF EXISTS platform_update_rollouts;
ALTER TABLE IF EXISTS agent_platform_update_jobs
    DROP CONSTRAINT IF EXISTS agent_platform_update_jobs_rollout_identity;
