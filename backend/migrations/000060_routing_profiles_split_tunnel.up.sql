CREATE TABLE routing_profile_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    routing_profile_id UUID NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 1000,
    action TEXT NOT NULL CHECK (action IN ('direct', 'vpn', 'block')),
    domains TEXT[] NOT NULL DEFAULT '{}'::text[],
    domain_suffixes TEXT[] NOT NULL DEFAULT '{}'::text[],
    domain_keywords TEXT[] NOT NULL DEFAULT '{}'::text[],
    ip_cidrs TEXT[] NOT NULL DEFAULT '{}'::text[],
    geo_sites TEXT[] NOT NULL DEFAULT '{}'::text[],
    geo_ips TEXT[] NOT NULL DEFAULT '{}'::text[],
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX routing_profile_rules_profile_priority_idx
    ON routing_profile_rules (routing_profile_id, priority, created_at);

CREATE TABLE server_routing_profiles (
    server_id UUID PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    routing_profile_id UUID NOT NULL REFERENCES routing_profiles(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX server_routing_profiles_profile_idx
    ON server_routing_profiles (routing_profile_id);

INSERT INTO routing_profiles (name, description, is_default)
SELECT 'Default direct', 'Built-in default profile. All traffic uses the VPN route unless explicit split-tunnel rules are added.', TRUE
WHERE NOT EXISTS (
    SELECT 1 FROM routing_profiles WHERE is_default = TRUE
);
