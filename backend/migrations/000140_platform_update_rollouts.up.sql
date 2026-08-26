CREATE TABLE platform_update_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT platform_update_rollouts_target_version_check CHECK (
        target_version ~ '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$'
    ),
    CONSTRAINT platform_update_rollouts_status_check CHECK (
        status IN ('pending', 'running', 'succeeded', 'failed', 'outcome_unknown')
    ),
    CONSTRAINT platform_update_rollouts_error_code_check CHECK (
        error_code IS NULL OR error_code ~ '^[a-z0-9_]{1,96}$'
    ),
    CONSTRAINT platform_update_rollouts_started_timestamp_check CHECK (
        (status = 'pending' AND started_at IS NULL)
        OR (status IN ('running', 'succeeded', 'failed', 'outcome_unknown') AND started_at IS NOT NULL)
    ),
    CONSTRAINT platform_update_rollouts_completed_timestamp_check CHECK (
        (status IN ('succeeded', 'failed', 'outcome_unknown') AND completed_at IS NOT NULL)
        OR (status IN ('pending', 'running') AND completed_at IS NULL)
    )
);

CREATE TABLE platform_update_rollout_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id UUID NOT NULL REFERENCES platform_update_rollouts(id) ON DELETE CASCADE,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    position INTEGER NOT NULL,
    platform_update_job_id UUID REFERENCES agent_platform_update_jobs(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'queued',
    blocker_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT platform_update_rollout_entries_position_check CHECK (position >= 0),
    CONSTRAINT platform_update_rollout_entries_status_check CHECK (
        status IN ('queued', 'waiting', 'updating', 'healthy', 'failed', 'outcome_unknown', 'skipped')
    ),
    CONSTRAINT platform_update_rollout_entries_blocker_code_check CHECK (
        blocker_code IS NULL OR blocker_code ~ '^[a-z0-9_]{1,96}$'
    ),
    CONSTRAINT platform_update_rollout_entries_job_check CHECK (
        (status IN ('queued', 'waiting', 'skipped') AND platform_update_job_id IS NULL)
        OR (status IN ('updating', 'healthy', 'failed', 'outcome_unknown') AND platform_update_job_id IS NOT NULL)
    ),
    CONSTRAINT platform_update_rollout_entries_completed_timestamp_check CHECK (
        (status IN ('healthy', 'failed', 'outcome_unknown', 'skipped') AND completed_at IS NOT NULL)
        OR (status IN ('queued', 'waiting', 'updating') AND completed_at IS NULL)
    ),
    CONSTRAINT platform_update_rollout_entries_unique_position UNIQUE (rollout_id, position),
    CONSTRAINT platform_update_rollout_entries_unique_server UNIQUE (rollout_id, server_id),
    CONSTRAINT platform_update_rollout_entries_unique_job UNIQUE (platform_update_job_id)
);

CREATE UNIQUE INDEX idx_platform_update_rollout_entries_one_updating
    ON platform_update_rollout_entries(rollout_id)
    WHERE status = 'updating';

CREATE INDEX idx_platform_update_rollouts_active
    ON platform_update_rollouts(updated_at, id)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_platform_update_rollout_entries_resume
    ON platform_update_rollout_entries(rollout_id, position)
    WHERE status IN ('queued', 'waiting', 'updating');
