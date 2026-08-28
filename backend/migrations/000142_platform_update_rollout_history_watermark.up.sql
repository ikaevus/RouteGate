ALTER TABLE platform_update_rollout_entries
    ADD COLUMN observed_update_job_count BIGINT;

ALTER TABLE platform_update_rollout_entries
    ADD CONSTRAINT platform_update_rollout_entries_observed_update_job_count_check CHECK (
        observed_update_job_count IS NULL OR observed_update_job_count >= 0
    );

-- All update-job inserts, including direct SQL writers and tests, must join the
-- same canonical per-server admission lock used by Manager code and rollout
-- snapshots. This makes the history watermark a database-level serialization
-- boundary rather than a convention that Go callers can accidentally bypass.
CREATE OR REPLACE FUNCTION lock_platform_update_job_admission()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
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
-- direct SQL/tests cannot fabricate the evidence either.
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
