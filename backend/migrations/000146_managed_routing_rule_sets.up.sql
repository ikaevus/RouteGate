ALTER TABLE routing_profiles ADD COLUMN default_action text NOT NULL DEFAULT 'vpn'
    CHECK (default_action IN ('direct', 'vpn', 'block'));
CREATE TABLE routing_profile_managed_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    routing_profile_id uuid NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    provider text NOT NULL,
    source_url text NOT NULL,
    priority integer NOT NULL DEFAULT 2000 CHECK (priority BETWEEN 0 AND 1000000),
    action text NOT NULL CHECK (action IN ('direct', 'vpn', 'block')),
    enabled boolean NOT NULL DEFAULT true,
    refresh_hours integer NOT NULL DEFAULT 24 CHECK (refresh_hours BETWEEN 1 AND 168),
    snapshot jsonb,
    snapshot_sha256 text NOT NULL DEFAULT '',
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX routing_profile_managed_sets_profile_idx ON routing_profile_managed_sets(routing_profile_id);
