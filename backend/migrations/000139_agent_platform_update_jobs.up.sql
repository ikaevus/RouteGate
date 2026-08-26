CREATE TABLE agent_platform_update_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    target_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    dispatched_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT agent_platform_update_jobs_target_version_check CHECK (
        target_version ~ '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$'
    ),
    CONSTRAINT agent_platform_update_jobs_status_check CHECK (
        status IN ('pending', 'in_progress', 'mutation_dispatched', 'succeeded', 'failed', 'outcome_unknown')
    ),
    CONSTRAINT agent_platform_update_jobs_error_code_check CHECK (
        error_code IS NULL OR error_code ~ '^[a-z0-9_]{1,96}$'
    ),
    CONSTRAINT agent_platform_update_jobs_dispatch_timestamp_check CHECK (
        (status IN ('mutation_dispatched', 'succeeded', 'failed', 'outcome_unknown') AND dispatched_at IS NOT NULL)
        OR (status IN ('pending', 'in_progress') AND dispatched_at IS NULL)
        OR (status = 'failed' AND dispatched_at IS NULL)
    ),
    CONSTRAINT agent_platform_update_jobs_completed_timestamp_check CHECK (
        (status IN ('succeeded', 'failed', 'outcome_unknown') AND completed_at IS NOT NULL)
        OR (status IN ('pending', 'in_progress', 'mutation_dispatched') AND completed_at IS NULL)
    )
);

CREATE UNIQUE INDEX idx_agent_platform_update_jobs_one_active_per_server
    ON agent_platform_update_jobs(server_id)
    WHERE status IN ('pending', 'in_progress', 'mutation_dispatched');

CREATE INDEX idx_agent_platform_update_jobs_reconcile
    ON agent_platform_update_jobs(updated_at, id)
    WHERE status = 'mutation_dispatched';

CREATE INDEX idx_agent_platform_update_jobs_created_at
    ON agent_platform_update_jobs(created_at DESC);
