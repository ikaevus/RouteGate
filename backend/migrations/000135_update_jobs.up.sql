CREATE TABLE update_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation TEXT NOT NULL,
    status TEXT NOT NULL,
    stage TEXT NOT NULL,
    request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT update_jobs_operation_check CHECK (operation IN ('preflight')),
    CONSTRAINT update_jobs_status_check CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    CONSTRAINT update_jobs_stage_check CHECK (stage IN ('preflight')),
    CONSTRAINT update_jobs_error_code_check CHECK (error_code IS NULL OR error_code ~ '^[a-z0-9_]{1,96}$')
);

CREATE INDEX idx_update_jobs_created_at ON update_jobs(created_at DESC);
CREATE INDEX idx_update_jobs_status_created_at ON update_jobs(status, created_at DESC);
