ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS protocol_version INTEGER;
