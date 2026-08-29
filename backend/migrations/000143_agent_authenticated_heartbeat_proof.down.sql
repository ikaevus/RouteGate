ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_authenticated_heartbeat_current_generation_check,
    DROP CONSTRAINT IF EXISTS agents_authenticated_heartbeat_proof_pair_check,
    DROP CONSTRAINT IF EXISTS agents_credential_generation_positive_check,
    DROP COLUMN IF EXISTS last_authenticated_heartbeat_generation,
    DROP COLUMN IF EXISTS last_authenticated_heartbeat_at,
    DROP COLUMN IF EXISTS credential_generation;
