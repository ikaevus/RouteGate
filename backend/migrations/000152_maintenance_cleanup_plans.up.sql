CREATE TABLE maintenance_cleanup_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    selected_categories TEXT[] NOT NULL,
    plan_payload JSONB NOT NULL,
    confirmation_token_hash BYTEA NOT NULL,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    report_payload JSONB,
    error_code TEXT,
    CONSTRAINT maintenance_cleanup_plans_mode_check
        CHECK (mode IN ('recommended', 'advanced')),
    CONSTRAINT maintenance_cleanup_plans_status_check
        CHECK (status IN ('planned', 'running', 'succeeded', 'failed', 'expired')),
    CONSTRAINT maintenance_cleanup_plans_categories_check
        CHECK (cardinality(selected_categories) > 0),
    CONSTRAINT maintenance_cleanup_plans_payload_check
        CHECK (jsonb_typeof(plan_payload) = 'object'),
    CONSTRAINT maintenance_cleanup_plans_report_check
        CHECK (report_payload IS NULL OR jsonb_typeof(report_payload) = 'object'),
    CONSTRAINT maintenance_cleanup_plans_token_hash_check
        CHECK (octet_length(confirmation_token_hash) = 32),
    CONSTRAINT maintenance_cleanup_plans_expiry_check
        CHECK (expires_at > created_at),
    CONSTRAINT maintenance_cleanup_plans_error_code_check
        CHECK (error_code IS NULL OR error_code ~ '^[a-z0-9_]{1,96}$')
);

CREATE INDEX idx_maintenance_cleanup_plans_created_at
    ON maintenance_cleanup_plans(created_at DESC);

CREATE UNIQUE INDEX idx_maintenance_cleanup_plans_one_running
    ON maintenance_cleanup_plans((true))
    WHERE status = 'running';

CREATE INDEX idx_maintenance_cleanup_plans_expiry
    ON maintenance_cleanup_plans(expires_at)
    WHERE status = 'planned';
