DROP TRIGGER IF EXISTS trg_platform_update_rollout_entries_transition ON platform_update_rollout_entries;
DROP FUNCTION IF EXISTS enforce_platform_update_rollout_entry_transition();

ALTER TABLE platform_update_rollout_entries
    DROP CONSTRAINT IF EXISTS platform_update_rollout_entries_observed_update_job_count_check,
    DROP COLUMN IF EXISTS observed_update_job_count;

-- Migration 000141 owns the previous transition function. Full migrator
-- rollback applies 000141 down immediately afterwards.
