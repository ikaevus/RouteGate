ALTER TABLE platform_update_rollout_entries
    ADD COLUMN observed_update_job_count BIGINT;

ALTER TABLE platform_update_rollout_entries
    ADD CONSTRAINT platform_update_rollout_entries_observed_update_job_count_check CHECK (
        observed_update_job_count IS NULL OR observed_update_job_count >= 0
    );

-- A per-server row count is a safe history watermark only if update-job identity
-- cannot disappear or move between servers. Preserve update jobs as immutable
-- security/audit history while still allowing their lifecycle fields to change.
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

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_agent_platform_update_jobs_history_identity
BEFORE UPDATE OR DELETE ON agent_platform_update_jobs
FOR EACH ROW
EXECUTE FUNCTION enforce_platform_update_job_history_identity();

-- Reset statement-local ordering evidence before any INSERT statement. The row
-- trigger below requires canonical server UUIDs to arrive in ascending order,
-- so concurrent multi-row raw SQL statements cannot acquire per-server advisory
-- locks in opposite orders and deadlock each other.
CREATE OR REPLACE FUNCTION reset_platform_update_job_admission_order()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM set_config('routegate.platform_update_job_last_server_id', '', true);
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_agent_platform_update_jobs_admission_order_reset
BEFORE INSERT ON agent_platform_update_jobs
FOR EACH STATEMENT
EXECUTE FUNCTION reset_platform_update_job_admission_order();

-- All update-job inserts, including direct SQL writers and tests, must join the
-- same canonical per-server admission lock used by Manager code and rollout
-- snapshots. Multi-row statements are accepted only in ascending canonical UUID
-- order, making lock acquisition deterministic across concurrent statements.
CREATE OR REPLACE FUNCTION lock_platform_update_job_admission()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
BEGIN
    previous_server_id := current_setting('routegate.platform_update_job_last_server_id', true);
    IF previous_server_id IS NOT NULL
        AND previous_server_id <> ''
        AND NEW.server_id < previous_server_id::uuid THEN
        RAISE EXCEPTION 'multi-row platform update job inserts must use ascending canonical server_id order';
    END IF;

    PERFORM set_config('routegate.platform_update_job_last_server_id', NEW.server_id::text, true);
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.server_id::text, 0));
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_agent_platform_update_jobs_admission_lock
BEFORE INSERT ON agent_platform_update_jobs
FOR EACH ROW
EXECUTE FUNCTION lock_platform_update_job_admission();

-- The update-history watermark is part of the immutable planning snapshot.
-- Legacy entries intentionally remain NULL: E3d must fail closed rather than
-- infer that jobs created between an older snapshot and this migration were
-- already observed. Every new entry derives its watermark in PostgreSQL while
-- holding the same canonical per-server advisory lock as update admission, so
-- direct SQL/tests cannot fabricate the evidence either. The job-history
-- identity trigger above makes this count monotonic for each server.
CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_entry_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status NOT IN ('queued', 'skipped') OR NEW.platform_update_job_id IS NOT NULL THEN
            RAISE EXCEPTION 'platform update rollout entry must be created as an unbound planning snapshot';
        END IF;
        IF (NEW.status = 'queued' AND cardinality(NEW.planning_blockers) <> 0)
            OR (NEW.status = 'skipped' AND cardinality(NEW.planning_blockers) = 0) THEN
            RAISE EXCEPTION 'platform update rollout entry planning evidence is inconsistent';
        END IF;

        -- Keep the structural trigger on the same lock order as rollout
        -- execution: parent rollout first, then the per-server update-admission
        -- lock. This lets direct SQL/planner retries serialize with admission
        -- instead of creating a rollout-row <-> advisory-lock deadlock cycle.
        PERFORM 1
        FROM platform_update_rollouts
        WHERE id = NEW.rollout_id
          AND target_version = NEW.target_version
          AND status = 'pending'
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'platform update rollout entries may only be added while parent is pending';
        END IF;

        PERFORM pg_advisory_xact_lock(hashtextextended(NEW.server_id::text, 0));
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

        -- E3d deliberately has no updating -> healthy transition yet. A later
        -- slice must introduce an atomic proof that the bound job succeeded and
        -- that fresh post-update health evidence exists before fleet advancement
        -- can authorize another host mutation.
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
