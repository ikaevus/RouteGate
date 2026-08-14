DROP TABLE IF EXISTS observability_diagnostic_runs;

ALTER TABLE agent_operation_jobs
    DROP CONSTRAINT agent_operation_jobs_kind_check,
    DROP CONSTRAINT agent_operation_jobs_operation_check;

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_kind_check
        CHECK (kind IN ('vpn_core_service', 'vpn_core_install')),
    ADD CONSTRAINT agent_operation_jobs_operation_check
        CHECK (
            (kind = 'vpn_core_service' AND operation IN ('start', 'stop', 'restart'))
            OR
            (kind = 'vpn_core_install' AND operation = 'install_sing_box')
        );
