CREATE TABLE agent_operation_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    operation TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    result_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_operation_jobs_kind_check
        CHECK (kind = 'vpn_core_service'),
    CONSTRAINT agent_operation_jobs_operation_check
        CHECK (operation IN ('start', 'stop', 'restart')),
    CONSTRAINT agent_operation_jobs_status_check
        CHECK (status IN ('pending', 'in_progress', 'succeeded', 'failed'))
);

CREATE INDEX idx_agent_operation_jobs_claim
    ON agent_operation_jobs (server_id, status, created_at);

CREATE UNIQUE INDEX idx_agent_operation_jobs_one_active_per_server
    ON agent_operation_jobs (server_id)
    WHERE status IN ('pending', 'in_progress');
