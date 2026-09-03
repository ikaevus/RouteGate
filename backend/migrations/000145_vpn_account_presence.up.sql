ALTER TABLE agents
    ADD COLUMN client_presence_observed_at TIMESTAMPTZ;

CREATE TABLE vpn_account_presence (
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    vpn_account_id UUID NOT NULL REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    protocol TEXT NOT NULL,
    connection_count INTEGER NOT NULL DEFAULT 1,
    source TEXT NOT NULL,
    confidence TEXT NOT NULL,
    connected_at TIMESTAMPTZ,
    last_activity_at TIMESTAMPTZ,
    observed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, vpn_account_id, protocol),
    CONSTRAINT vpn_account_presence_protocol_not_blank CHECK (btrim(protocol) <> ''),
    CONSTRAINT vpn_account_presence_connection_count_positive CHECK (connection_count > 0),
    CONSTRAINT vpn_account_presence_source_not_blank CHECK (btrim(source) <> ''),
    CONSTRAINT vpn_account_presence_confidence_check CHECK (confidence IN ('exact', 'heuristic')),
    CONSTRAINT vpn_account_presence_expiry_check CHECK (expires_at > observed_at)
);

CREATE INDEX idx_vpn_account_presence_live
    ON vpn_account_presence(expires_at DESC, observed_at DESC);

CREATE INDEX idx_vpn_account_presence_server_live
    ON vpn_account_presence(server_id, expires_at DESC);
