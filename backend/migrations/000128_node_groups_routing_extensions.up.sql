CREATE TABLE node_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    selection_strategy TEXT NOT NULL DEFAULT 'priority',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT node_groups_name_check CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    CONSTRAINT node_groups_description_check CHECK (description IS NULL OR length(description) <= 500),
    CONSTRAINT node_groups_selection_strategy_check CHECK (selection_strategy IN ('priority', 'weighted'))
);

CREATE UNIQUE INDEX node_groups_name_ci_unique
    ON node_groups (lower(btrim(name)));

CREATE TABLE node_group_members (
    node_group_id UUID NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 100,
    weight INTEGER NOT NULL DEFAULT 100,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (node_group_id, server_id),
    CONSTRAINT node_group_members_priority_check CHECK (priority BETWEEN 0 AND 10000),
    CONSTRAINT node_group_members_weight_check CHECK (weight BETWEEN 1 AND 1000)
);

CREATE INDEX idx_node_group_members_server_id
    ON node_group_members(server_id);

CREATE TABLE vpn_account_routing_profiles (
    vpn_account_id UUID PRIMARY KEY REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    routing_profile_id UUID NOT NULL REFERENCES routing_profiles(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vpn_account_routing_profiles_profile_id
    ON vpn_account_routing_profiles(routing_profile_id);

CREATE TABLE vpn_account_node_groups (
    vpn_account_id UUID PRIMARY KEY REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    node_group_id UUID NOT NULL REFERENCES node_groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vpn_account_node_groups_group_id
    ON vpn_account_node_groups(node_group_id);
