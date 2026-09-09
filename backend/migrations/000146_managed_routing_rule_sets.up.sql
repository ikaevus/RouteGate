ALTER TABLE routing_profiles
    ADD COLUMN default_action TEXT NOT NULL DEFAULT 'vpn'
        CHECK (default_action IN ('direct', 'vpn', 'block'));

CREATE TABLE managed_routing_rule_sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    routing_profile_id UUID NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'custom',
    source_url TEXT NOT NULL,
    source_format TEXT NOT NULL DEFAULT 'source' CHECK (source_format IN ('source')),
    priority INTEGER NOT NULL DEFAULT 1000 CHECK (priority BETWEEN 0 AND 1000000),
    action TEXT NOT NULL CHECK (action IN ('direct', 'vpn', 'block')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    refresh_interval_hours INTEGER NOT NULL DEFAULT 24 CHECK (refresh_interval_hours BETWEEN 1 AND 720),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'healthy', 'error')),
    last_refresh_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT,
    snapshot JSONB,
    snapshot_sha256 TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT managed_routing_rule_sets_name_check CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    CONSTRAINT managed_routing_rule_sets_provider_check CHECK (length(btrim(provider)) BETWEEN 1 AND 80),
    CONSTRAINT managed_routing_rule_sets_source_url_check CHECK (length(btrim(source_url)) BETWEEN 1 AND 2048)
);

CREATE INDEX managed_routing_rule_sets_profile_priority_idx
    ON managed_routing_rule_sets (routing_profile_id, priority, created_at, id);

CREATE INDEX managed_routing_rule_sets_refresh_due_idx
    ON managed_routing_rule_sets (enabled, last_success_at);
