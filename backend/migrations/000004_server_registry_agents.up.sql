ALTER TABLE servers
    ADD COLUMN description TEXT,
    ADD COLUMN private_ip INET;

UPDATE servers
SET status = CASE status
    WHEN 'unknown' THEN 'pending'
    WHEN 'online' THEN 'active'
    ELSE status
END;

ALTER TABLE servers
    ALTER COLUMN status SET DEFAULT 'pending',
    ADD CONSTRAINT servers_name_key UNIQUE (name),
    ADD CONSTRAINT servers_status_check CHECK (
        status IN ('pending', 'active', 'offline', 'disabled', 'error')
    );

CREATE INDEX idx_servers_status ON servers(status);
CREATE INDEX idx_servers_provider ON servers(provider);
CREATE INDEX idx_servers_location ON servers(location);

ALTER TABLE agents
    ADD COLUMN os TEXT,
    ADD COLUMN arch TEXT,
    ADD COLUMN agent_version TEXT,
    ADD COLUMN token_hash TEXT,
    ADD COLUMN capabilities JSONB,
    ADD COLUMN registered_at TIMESTAMPTZ;

UPDATE agents
SET
    agent_version = COALESCE(NULLIF(version, ''), '0.1.0'),
    token_hash = COALESCE(
        NULLIF(agent_key_hash, ''),
        encode(digest('legacy-agent:' || id::text, 'sha256'), 'hex')
    ),
    capabilities = '{}'::jsonb,
    registered_at = created_at,
    status = CASE status
        WHEN 'new' THEN 'registered'
        ELSE status
    END;

ALTER TABLE agents
    DROP CONSTRAINT agents_server_id_fkey,
    ALTER COLUMN server_id SET NOT NULL,
    ALTER COLUMN agent_version SET NOT NULL,
    ALTER COLUMN token_hash SET NOT NULL,
    ALTER COLUMN capabilities SET DEFAULT '{}'::jsonb,
    ALTER COLUMN capabilities SET NOT NULL,
    ALTER COLUMN registered_at SET DEFAULT now(),
    ALTER COLUMN registered_at SET NOT NULL,
    ALTER COLUMN status SET DEFAULT 'registered',
    ADD CONSTRAINT agents_server_id_fkey
        FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE,
    ADD CONSTRAINT agents_server_id_key UNIQUE (server_id),
    ADD CONSTRAINT agents_status_check CHECK (
        status IN ('registered', 'online', 'offline', 'disabled', 'error')
    );

CREATE INDEX idx_agents_status ON agents(status);
CREATE INDEX idx_agents_capabilities ON agents USING GIN(capabilities);

CREATE TABLE server_registration_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_server_registration_tokens_server_id
    ON server_registration_tokens(server_id);
CREATE UNIQUE INDEX idx_server_registration_tokens_token_hash
    ON server_registration_tokens(token_hash);
CREATE INDEX idx_server_registration_tokens_expires_at
    ON server_registration_tokens(expires_at);
CREATE INDEX idx_server_registration_tokens_unused
    ON server_registration_tokens(server_id, created_at)
    WHERE used_at IS NULL;
