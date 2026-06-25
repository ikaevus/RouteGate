CREATE TABLE config_apply_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    config_version_id UUID NOT NULL REFERENCES config_versions(id) ON DELETE CASCADE,
    action TEXT NOT NULL DEFAULT 'apply',
    status TEXT NOT NULL DEFAULT 'pending',
    request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_config_apply_jobs_server_status ON config_apply_jobs(server_id, status, created_at);
CREATE INDEX idx_config_apply_jobs_agent_status ON config_apply_jobs(agent_id, status, created_at);
CREATE INDEX idx_config_apply_jobs_config_version ON config_apply_jobs(config_version_id);
