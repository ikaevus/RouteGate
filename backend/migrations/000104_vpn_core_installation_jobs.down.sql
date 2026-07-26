DELETE FROM agent_operation_jobs
WHERE kind = 'vpn_core_install';

ALTER TABLE agent_operation_jobs
    DROP CONSTRAINT agent_operation_jobs_kind_check,
    DROP CONSTRAINT agent_operation_jobs_operation_check;

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_kind_check
        CHECK (kind = 'vpn_core_service'),
    ADD CONSTRAINT agent_operation_jobs_operation_check
        CHECK (operation IN ('start', 'stop', 'restart'));
