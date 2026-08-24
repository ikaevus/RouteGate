DELETE FROM update_jobs
WHERE operation = 'discovery' OR stage = 'discovery';

ALTER TABLE update_jobs
    DROP CONSTRAINT IF EXISTS update_jobs_operation_check;

ALTER TABLE update_jobs
    ADD CONSTRAINT update_jobs_operation_check
    CHECK (operation IN ('preflight'));

ALTER TABLE update_jobs
    DROP CONSTRAINT IF EXISTS update_jobs_stage_check;

ALTER TABLE update_jobs
    ADD CONSTRAINT update_jobs_stage_check
    CHECK (stage IN ('preflight'));
