-- E3e health advancement compares an authenticated heartbeat to the exact bound
-- update job's terminal completion. PostgreSQL transaction time (now()) can be
-- older than a row-lock wait, so the database boundary owns terminal completion
-- with clock_timestamp() at the actual forward lifecycle transition. Once a job
-- is terminal, its causal/provenance timestamps and error outcome are immutable.
CREATE OR REPLACE FUNCTION enforce_platform_update_job_history_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    entering_terminal BOOLEAN;
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

    IF OLD.status IN ('succeeded', 'failed', 'outcome_unknown') THEN
        IF NEW.status IS DISTINCT FROM OLD.status THEN
            RAISE EXCEPTION 'terminal platform update job status is immutable';
        END IF;
        IF NEW.started_at IS DISTINCT FROM OLD.started_at
            OR NEW.dispatched_at IS DISTINCT FROM OLD.dispatched_at
            OR NEW.completed_at IS DISTINCT FROM OLD.completed_at
            OR NEW.error_code IS DISTINCT FROM OLD.error_code THEN
            RAISE EXCEPTION 'terminal platform update job provenance is immutable';
        END IF;
        RETURN NEW;
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

    entering_terminal := NEW.status IN ('succeeded', 'failed', 'outcome_unknown')
        AND NEW.status IS DISTINCT FROM OLD.status;
    IF entering_terminal THEN
        -- Deliberately ignore any caller-supplied completion value. This is the
        -- wall-clock instant at which PostgreSQL actually owns the terminal row
        -- transition after all row-lock waiting has completed.
        NEW.completed_at := clock_timestamp();
    END IF;

    RETURN NEW;
END;
$$;
