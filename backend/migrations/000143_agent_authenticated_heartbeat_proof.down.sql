-- Restore the E3d fail-closed rollout-entry state machine before removing the
-- authenticated-heartbeat proof columns introduced by migration 143.
CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_entry_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
    transaction_rollout_id TEXT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status NOT IN ('queued', 'skipped') OR NEW.platform_update_job_id IS NOT NULL THEN
            RAISE EXCEPTION 'platform update rollout entry must be created as an unbound planning snapshot';
        END IF;
        IF (NEW.status = 'queued' AND cardinality(NEW.planning_blockers) <> 0)
            OR (NEW.status = 'skipped' AND cardinality(NEW.planning_blockers) = 0) THEN
            RAISE EXCEPTION 'platform update rollout entry planning evidence is inconsistent';
        END IF;

        PERFORM lock_platform_update_admission_global();

        transaction_rollout_id := current_setting('routegate.platform_update_admission_rollout_id', true);
        previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
        IF transaction_rollout_id IS NULL OR transaction_rollout_id = '' THEN
            IF previous_server_id IS NOT NULL AND previous_server_id <> '' THEN
                RAISE EXCEPTION 'platform update rollout parent must be established before server admission locks';
            END IF;
        ELSIF transaction_rollout_id <> NEW.rollout_id::text THEN
            RAISE EXCEPTION 'platform update rollout entry inserts in one transaction must use one rollout parent';
        END IF;

        PERFORM 1
        FROM platform_update_rollouts
        WHERE id = NEW.rollout_id
          AND target_version = NEW.target_version
          AND status = 'pending'
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'platform update rollout entries may only be added while parent is pending';
        END IF;
        IF transaction_rollout_id IS NULL OR transaction_rollout_id = '' THEN
            PERFORM set_config('routegate.platform_update_admission_rollout_id', NEW.rollout_id::text, true);
        END IF;

        previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
        IF previous_server_id IS NOT NULL
            AND previous_server_id <> ''
            AND NEW.server_id < previous_server_id::uuid THEN
            RAISE EXCEPTION 'platform update rollout entry inserts in one transaction must use ascending canonical server_id order';
        END IF;

        PERFORM set_config('routegate.platform_update_admission_last_server_id', NEW.server_id::text, true);
        PERFORM pg_advisory_xact_lock(hashtextextended(NEW.server_id::text, 0));

        IF NEW.status = 'queued' THEN
            PERFORM 1
            FROM agent_platform_update_jobs
            WHERE server_id = NEW.server_id
              AND status IN ('pending', 'in_progress', 'mutation_dispatched', 'outcome_unknown')
            LIMIT 1;
            IF FOUND THEN
                RAISE EXCEPTION 'queued platform update rollout entry cannot snapshot a server with an active or unresolved update job';
            END IF;
        END IF;

        SELECT count(*)
        INTO NEW.observed_update_job_count
        FROM agent_platform_update_jobs
        WHERE server_id = NEW.server_id;
        RETURN NEW;
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.rollout_id IS DISTINCT FROM OLD.rollout_id
        OR NEW.server_id IS DISTINCT FROM OLD.server_id
        OR NEW.target_version IS DISTINCT FROM OLD.target_version
        OR NEW.position IS DISTINCT FROM OLD.position
        OR NEW.planning_blockers IS DISTINCT FROM OLD.planning_blockers
        OR NEW.observed_update_job_count IS DISTINCT FROM OLD.observed_update_job_count THEN
        RAISE EXCEPTION 'platform update rollout entry identity and planning evidence are immutable';
    END IF;

    IF OLD.platform_update_job_id IS NOT NULL
        AND NEW.platform_update_job_id IS DISTINCT FROM OLD.platform_update_job_id THEN
        RAISE EXCEPTION 'platform update rollout entry job identity is immutable once bound';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status THEN
        IF NOT (
            (OLD.status = 'queued' AND NEW.status IN ('waiting', 'updating'))
            OR (OLD.status = 'waiting' AND NEW.status = 'updating')
            OR (OLD.status = 'updating' AND NEW.status IN ('failed', 'outcome_unknown'))
        ) THEN
            RAISE EXCEPTION 'invalid platform update rollout entry transition % -> %', OLD.status, NEW.status;
        END IF;

        IF NEW.status = 'updating' THEN
            PERFORM 1
            FROM platform_update_rollouts
            WHERE id = OLD.rollout_id
              AND target_version = OLD.target_version
              AND status = 'running'
            FOR UPDATE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'platform update rollout entry can only begin updating while parent is running';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_authenticated_heartbeat_current_generation_check,
    DROP CONSTRAINT IF EXISTS agents_authenticated_heartbeat_proof_pair_check,
    DROP CONSTRAINT IF EXISTS agents_credential_generation_positive_check,
    DROP COLUMN IF EXISTS last_authenticated_heartbeat_generation,
    DROP COLUMN IF EXISTS last_authenticated_heartbeat_at,
    DROP COLUMN IF EXISTS credential_generation;
