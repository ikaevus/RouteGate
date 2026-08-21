CREATE TABLE IF NOT EXISTS auth_security_event_visibility (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    cleared_before TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE auth_security_event_visibility IS
    'Per-user UI visibility watermark for recent auth security events. Audit events remain immutable.';
