DROP TRIGGER IF EXISTS trg_agent_platform_update_jobs_admission_lock ON agent_platform_update_jobs;
DROP FUNCTION IF EXISTS lock_platform_update_job_admission();

DROP TRIGGER IF EXISTS trg_agent_platform_update_jobs_admission_order_reset ON agent_platform_update_jobs;
DROP FUNCTION IF EXISTS reset_platform_update_job_admission_order();

DROP TRIGGER IF EXISTS trg_agent_platform_update_jobs_history_identity ON agent_platform_update_jobs;
DROP FUNCTION IF EXISTS enforce_platform_update_job_history_identity();

DROP TRIGGER IF EXISTS trg_platform_update_rollout_entries_transition ON platform_update_rollout_entries;
DROP FUNCTION IF EXISTS enforce_platform_update_rollout_entry_transition();

ALTER TABLE platform_update_rollout_entries
    DROP CONSTRAINT IF EXISTS platform_update_rollout_entries_observed_update_job_count_check,
    DROP COLUMN IF EXISTS observed_update_job_count;

-- Migration 000141 owns the previous transition function. Full migrator
-- rollback applies 000141 down immediately afterwards.
