DROP TRIGGER IF EXISTS trg_platform_update_rollout_entries_transition ON platform_update_rollout_entries;
DROP FUNCTION IF EXISTS enforce_platform_update_rollout_entry_transition();

ALTER TABLE platform_update_rollout_entries
    DROP CONSTRAINT IF EXISTS platform_update_rollout_entries_planning_snapshot_check,
    DROP CONSTRAINT IF EXISTS platform_update_rollout_entries_planning_blockers_check,
    DROP COLUMN IF EXISTS planning_blockers;

-- Migration 000140 owns the original transition function/trigger. A down
-- migration intentionally leaves 000141 without recreating it; migrator
-- rollback is only used in tests and full schema rollback applies 000140 down
-- immediately afterwards.
