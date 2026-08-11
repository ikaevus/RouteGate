CREATE INDEX IF NOT EXISTS idx_config_apply_jobs_created_at_id
    ON config_apply_jobs(created_at DESC, id DESC);
