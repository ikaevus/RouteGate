-- Restore the pre-RG-114 operation constraint.

ALTER TABLE agent_operation_jobs
    DROP CONSTRAINT IF EXISTS agent_operation_jobs_operation_check;

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_operation_check
    CHECK (
        (kind = 'vpn_core_service' AND operation IN ('start', 'stop', 'restart'))
        OR
        (kind = 'vpn_core_install' AND operation = 'install_sing_box')
        OR
        (kind = 'diagnostic' AND operation IN ('host_overview', 'vpn_core_status'))
    );
