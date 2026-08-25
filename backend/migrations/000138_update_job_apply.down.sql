DROP INDEX IF EXISTS update_jobs_apply_stage_job_unique;

ALTER TABLE update_jobs
    DROP CONSTRAINT IF EXISTS update_jobs_operation_check;

ALTER TABLE update_jobs
    ADD CONSTRAINT update_jobs_operation_check
    CHECK (operation IN ('preflight', 'discovery', 'stage'));

ALTER TABLE update_jobs
    DROP CONSTRAINT IF EXISTS update_jobs_stage_check;

ALTER TABLE update_jobs
    ADD CONSTRAINT update_jobs_stage_check
    CHECK (stage IN ('preflight', 'discovery', 'stage'));
