ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT 'Unnamed Agent',
    ADD COLUMN IF NOT EXISTS hostname TEXT;

CREATE INDEX IF NOT EXISTS idx_agents_server_id ON agents(server_id);
CREATE INDEX IF NOT EXISTS idx_agents_last_seen_at ON agents(last_seen_at);
