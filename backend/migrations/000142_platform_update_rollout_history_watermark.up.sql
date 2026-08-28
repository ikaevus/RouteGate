ALTER TABLE platform_update_rollout_entries
    ADD COLUMN observed_update_job_count BIGINT;

ALTER TABLE platform_update_rollout_entries
    ADD CONSTRAINT platform_update_rollout_entries_observed_update_job_count_check CHECK (
        observed_update_job_count IS NULL OR observed_update_job_count >= 0
    );

-- A per-server row count is a safe history watermark only if update-job identity
-- cannot disappear or move between servers and a terminal row cannot be made
-- dispatch-capable again. Preserve update jobs as immutable security/audit
-- history and enforce the Manager-side forward-only lifecycle in PostgreSQL.
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
            (OLD.status = 'pending' AND NEW.status = 'in_progress')
            OR (OLD.status = 'in_progress' AND NEW.status IN ('mutation_dispatched', 'succeeded', 'failed', 'outcome_unknown'))
            OR (OLD.status = 'mutation_dispatched' AND NEW.status IN ('succeeded', 'failed', 'outcome_unknown'))
        ) THEN
            RAISE EXCEPTION 'invalid platform update job transition % -> %', OLD.status, NEW.status;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_agent_platform_update_jobs_history_identity
BEFORE UPDATE OR DELETE ON agent_platform_update_jobs
FOR EACH ROW
EXECUTE FUNCTION enforce_platform_update_job_history_identity();

-- All server-scoped update admission at the database boundary shares one
-- transaction-local ordering proof. This matters across both direct update-job
-- INSERTs and rollout-entry INSERTs: a transaction that already acquired a lock
-- for server B must not later try to acquire server A when A < B, regardless of
-- which table caused either lock.
CREATE OR REPLACE FUNCTION lock_platform_update_job_admission()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
BEGIN
    previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
    IF previous_server_id IS NOT NULL
        AND previous_server_id <> ''
        AND NEW.server_id < previous_server_id::uuid THEN
        RAISE EXCEPTION 'platform update job inserts in one transaction must use ascending canonical server_id order';
    END IF;

    PERFORM set_config('routegate.platform_update_admission_last_server_id', NEW.server_id::text, true);
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
-- identity/lifecycle trigger above makes this count monotonic for each server.
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

        -- A rollout-entry transaction must establish exactly one parent before
        -- it acquires any trigger-managed server admission lock. Otherwise a
        -- transaction can retain a server lock for one row and then wait for a
        -- second parent while a peer holds that parent and waits for the same
        -- server lock. Reject cross-parent and job-first mixed transactions
        -- before taking a new parent row lock.
        transaction_rollout_id := current_setting('routegate.platform_update_admission_rollout_id', true);
        previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
        IF transaction_rollout_id IS NULL OR transaction_rollout_id = '' THEN
            IF previous_server_id IS NOT NULL AND previous_server_id <> '' THEN
                RAISE EXCEPTION 'platform update rollout parent must be established before server admission locks';
            END IF;
        ELSIF transaction_rollout_id <> NEW.rollout_id::text THEN
            RAISE EXCEPTION 'platform update rollout entry inserts in one transaction must use one rollout parent';
        END IF;

        -- Keep the structural trigger on the same lock order as rollout
        -- execution: parent rollout first, then the per-server update-admission
        -- lock. Rechecking the same parent is safe because its row lock is
        -- retained until transaction end; a different parent was rejected above.
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

        -- After the parent is stable, join the same transaction-wide canonical
        -- server ordering used by update-job INSERTs. This prevents raw SQL
        -- transactions from acquiring overlapping server locks in opposite order.
        previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
        IF previous_server_id IS NOT NULL
            AND previous_server_id <> ''
            AND NEW.server_id < previous_server_id::uuid THEN
            RAISE EXCEPTION 'platform update rollout entry inserts in one transaction must use ascending canonical server_id order';
        END IF;

        PERFORM set_config('routegate.platform_update_admission_last_server_id', NEW.server_id::text, true);
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
