DROP TABLE IF EXISTS server_registration_tokens;

DROP INDEX IF EXISTS idx_agents_capabilities;
DROP INDEX IF EXISTS idx_agents_status;

ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_status_check,
    DROP CONSTRAINT IF EXISTS agents_server_id_key,
    DROP CONSTRAINT IF EXISTS agents_server_id_fkey,
    ALTER COLUMN server_id DROP NOT NULL,
    ALTER COLUMN status SET DEFAULT 'new';

UPDATE agents
SET
    agent_key_hash = token_hash,
    version = agent_version,
    status = CASE status
        WHEN 'registered' THEN 'new'
        ELSE status
    END;

ALTER TABLE agents
    ADD CONSTRAINT agents_server_id_fkey
        FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE SET NULL,
    DROP COLUMN IF EXISTS registered_at,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS token_hash,
    DROP COLUMN IF EXISTS agent_version,
    DROP COLUMN IF EXISTS arch,
    DROP COLUMN IF EXISTS os;

DROP INDEX IF EXISTS idx_servers_location;
DROP INDEX IF EXISTS idx_servers_provider;
DROP INDEX IF EXISTS idx_servers_status;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_status_check,
    DROP CONSTRAINT IF EXISTS servers_name_key,
    ALTER COLUMN status SET DEFAULT 'unknown';

UPDATE servers
SET status = CASE status
    WHEN 'pending' THEN 'unknown'
    WHEN 'active' THEN 'online'
    ELSE status
END;

ALTER TABLE servers
    DROP COLUMN IF EXISTS private_ip,
    DROP COLUMN IF EXISTS description;
