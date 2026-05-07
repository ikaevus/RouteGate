DROP INDEX IF EXISTS idx_agents_last_seen_at;
DROP INDEX IF EXISTS idx_agents_server_id;

ALTER TABLE agents
    DROP COLUMN IF EXISTS hostname,
    DROP COLUMN IF EXISTS name;
