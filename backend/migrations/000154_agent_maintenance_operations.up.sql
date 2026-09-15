ALTER TABLE agent_operation_jobs
    ADD COLUMN request_payload JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent_operation_jobs
    DROP CONSTRAINT IF EXISTS agent_operation_jobs_kind_check,
    DROP CONSTRAINT IF EXISTS agent_operation_jobs_operation_check;

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_kind_check
        CHECK (kind IN ('vpn_core_service', 'vpn_core_install', 'diagnostic', 'maintenance')),
    ADD CONSTRAINT agent_operation_jobs_operation_check
        CHECK (
            (kind = 'vpn_core_service' AND operation IN ('start', 'stop', 'restart'))
            OR
            (kind = 'vpn_core_install' AND operation IN (
                'install_sing_box',
                'install_wireguard',
                'install_hysteria2',
                'install_mtg'
            ))
            OR
            (kind = 'diagnostic' AND operation IN (
                'host_overview',
                'vpn_core_status',
                'manager_certificate'
            ))
            OR
            (kind = 'maintenance' AND operation IN (
                'analyze_runtime_artifacts',
                'cleanup_runtime_artifacts',
                'verify_runtime_artifacts'
            ))
        );

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_request_payload_object_check
        CHECK (jsonb_typeof(request_payload) = 'object'),
    ADD CONSTRAINT agent_operation_jobs_request_payload_scope_check
        CHECK (kind = 'maintenance' OR request_payload = '{}'::jsonb);
