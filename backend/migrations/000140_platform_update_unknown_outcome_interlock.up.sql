DROP INDEX IF EXISTS idx_agent_platform_update_jobs_one_active_per_server;

CREATE UNIQUE INDEX idx_agent_platform_update_jobs_one_active_per_server
    ON agent_platform_update_jobs(server_id)
    WHERE status IN ('pending', 'in_progress', 'mutation_dispatched', 'outcome_unknown');
