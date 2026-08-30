-- Restore the migration-142 job-history guard when rolling the schema-143
-- terminal timestamp proof back.
CREATE OR REPLACE FUNCTION enforce_platform_update_job_history_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'platform update job history is immutable and cannot be deleted';
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.server_id IS DISTINCT FROM OLD.server_id
        OR NEW.agent_id IS DISTINCT FROM OLD.agent_id
        OR NEW.target_version IS DISTINCT FROM OLD.target_version
        OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'platform update job identity is immutable';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status THEN
        IF NOT (
            (OLD.status = 'pending' AND NEW.status IN ('in_progress', 'outcome_unknown'))
            OR (OLD.status = 'in_progress' AND NEW.status IN ('mutation_dispatched', 'succeeded', 'failed', 'outcome_unknown'))
            OR (OLD.status = 'mutation_dispatched' AND NEW.status IN ('succeeded', 'failed', 'outcome_unknown'))
        ) THEN
            RAISE EXCEPTION 'invalid platform update job transition % -> %', OLD.status, NEW.status;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;
