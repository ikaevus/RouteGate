ALTER TABLE platform_update_rollouts
    ADD COLUMN creation_idempotency_key UUID,
    ADD COLUMN creation_request_hash TEXT,
    ADD CONSTRAINT platform_update_rollouts_creation_evidence_check CHECK (
        (creation_idempotency_key IS NULL AND creation_request_hash IS NULL)
        OR (creation_idempotency_key IS NOT NULL AND creation_request_hash ~ '^[0-9a-f]{64}$')
    ),
    ADD CONSTRAINT platform_update_rollouts_creation_idempotency_key_key UNIQUE (creation_idempotency_key);

CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_creation_evidence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.creation_idempotency_key IS DISTINCT FROM OLD.creation_idempotency_key
        OR NEW.creation_request_hash IS DISTINCT FROM OLD.creation_request_hash THEN
        RAISE EXCEPTION 'platform update rollout creation evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER platform_update_rollouts_creation_evidence_immutable
BEFORE UPDATE ON platform_update_rollouts
FOR EACH ROW EXECUTE FUNCTION enforce_platform_update_rollout_creation_evidence();
