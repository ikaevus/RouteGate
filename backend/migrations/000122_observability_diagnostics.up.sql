ALTER TABLE agent_operation_jobs
    DROP CONSTRAINT agent_operation_jobs_kind_check,
    DROP CONSTRAINT agent_operation_jobs_operation_check;

ALTER TABLE agent_operation_jobs
    ADD CONSTRAINT agent_operation_jobs_kind_check
        CHECK (kind IN ('vpn_core_service', 'vpn_core_install', 'diagnostic')),
    ADD CONSTRAINT agent_operation_jobs_operation_check
        CHECK (
            (kind = 'vpn_core_service' AND operation IN ('start', 'stop', 'restart'))
            OR
            (kind = 'vpn_core_install' AND operation = 'install_sing_box')
            OR
            (kind = 'diagnostic' AND operation IN ('host_overview', 'vpn_core_status'))
        );

CREATE TABLE observability_diagnostic_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    agent_operation_job_id UUID UNIQUE REFERENCES agent_operation_jobs(id) ON DELETE SET NULL,
    profile_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    state TEXT,
    result_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason_code TEXT,
    summary TEXT,
    recommended_action TEXT,
    error_message TEXT,
    requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_diagnostic_runs_profile_check
        CHECK (profile_key IN ('host_overview', 'vpn_core_status')),
    CONSTRAINT observability_diagnostic_runs_status_check
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    CONSTRAINT observability_diagnostic_runs_state_check
        CHECK (state IS NULL OR state IN ('healthy', 'degraded', 'unhealthy', 'unknown')),
    CONSTRAINT observability_diagnostic_runs_result_object_check
        CHECK (jsonb_typeof(result_payload) = 'object'),
    CONSTRAINT observability_diagnostic_runs_reason_not_blank
        CHECK (reason_code IS NULL OR btrim(reason_code) <> ''),
    CONSTRAINT observability_diagnostic_runs_summary_not_blank
        CHECK (summary IS NULL OR btrim(summary) <> ''),
    CONSTRAINT observability_diagnostic_runs_action_not_blank
        CHECK (recommended_action IS NULL OR btrim(recommended_action) <> ''),
    CONSTRAINT observability_diagnostic_runs_error_not_blank
        CHECK (error_message IS NULL OR btrim(error_message) <> ''),
    CONSTRAINT observability_diagnostic_runs_time_order_check
        CHECK (
            (started_at IS NULL OR started_at >= requested_at)
            AND (completed_at IS NULL OR completed_at >= requested_at)
            AND (completed_at IS NULL OR started_at IS NULL OR completed_at >= started_at)
        )
);

CREATE INDEX idx_observability_diagnostic_runs_server_history
    ON observability_diagnostic_runs (server_id, requested_at DESC, id DESC);

CREATE INDEX idx_observability_diagnostic_runs_active
    ON observability_diagnostic_runs (status, requested_at, id)
    WHERE status IN ('queued', 'running');
