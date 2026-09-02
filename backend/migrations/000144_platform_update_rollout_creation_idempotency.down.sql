DROP TRIGGER IF EXISTS platform_update_rollouts_creation_evidence_immutable ON platform_update_rollouts;
DROP FUNCTION IF EXISTS enforce_platform_update_rollout_creation_evidence();
ALTER TABLE platform_update_rollouts
    DROP CONSTRAINT IF EXISTS platform_update_rollouts_creation_idempotency_key_key,
    DROP CONSTRAINT IF EXISTS platform_update_rollouts_creation_evidence_check,
    DROP COLUMN IF EXISTS creation_request_hash,
    DROP COLUMN IF EXISTS creation_idempotency_key;
