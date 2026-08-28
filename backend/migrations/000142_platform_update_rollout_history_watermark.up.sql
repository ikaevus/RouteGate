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

CREATE TRIGGER trg_agent_platform_update_jobs_history_identity
BEFORE UPDATE OR DELETE ON agent_platform_update_jobs
FOR EACH ROW
EXECUTE FUNCTION enforce_platform_update_job_history_identity();

-- All platform-update admission transactions take this short-lived global
-- transaction lock before any rollout-parent or per-server admission lock. The
-- host mutation itself happens after commit, so this serializes only the durable
-- admission/binding decision while eliminating parent<->server lock inversions
-- for Manager code and raw SQL writers at the PostgreSQL boundary.
CREATE OR REPLACE FUNCTION lock_platform_update_admission_global()
RETURNS void
LANGUAGE sql
AS $$
    SELECT pg_advisory_xact_lock(722096142::bigint);
$$;

-- All server-scoped update admission at the database boundary shares one
-- transaction-local ordering proof. The global admission mutex above prevents
-- cross-transaction deadlocks; the ascending proof remains a fail-closed
-- structural check for accidental unordered multi-server SQL in one transaction.
CREATE OR REPLACE FUNCTION lock_platform_update_job_admission()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
BEGIN
    PERFORM lock_platform_update_admission_global();

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

-- UPDATE statements join the same global admission mutex before PostgreSQL can
-- acquire target rollout-entry row locks. The legacy transaction-local marker
-- remains only a fail-closed policy check for ordinary raw SQL; it is no longer
-- trusted as lock-order evidence, so a caller-written set_config cannot recreate
-- a parent/server deadlock.
CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_update_lock_order()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
    transaction_rollout_id TEXT;
BEGIN
    PERFORM lock_platform_update_admission_global();

    previous_server_id := current_setting('routegate.platform_update_admission_last_server_id', true);
    transaction_rollout_id := current_setting('routegate.platform_update_admission_rollout_id', true);
    IF previous_server_id IS NOT NULL
        AND previous_server_id <> ''
        AND (transaction_rollout_id IS NULL OR transaction_rollout_id = '') THEN
        RAISE EXCEPTION 'platform update rollout parent must be established before binding update after server admission lock';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_platform_update_rollout_entries_update_lock_order
BEFORE UPDATE ON platform_update_rollout_entries
FOR EACH STATEMENT
EXECUTE FUNCTION enforce_platform_update_rollout_update_lock_order();

-- Parent updates must also join the global admission mutex before PostgreSQL
-- acquires the parent row lock. This covers raw job-first transactions that try
-- to start/terminalize a rollout before binding an entry: peers can never hold a
-- parent while waiting for a server lock owned by that transaction.
CREATE OR REPLACE FUNCTION lock_platform_update_rollout_parent_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM lock_platform_update_admission_global();
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_platform_update_rollouts_update_admission_lock
BEFORE UPDATE ON platform_update_rollouts
FOR EACH STATEMENT
EXECUTE FUNCTION lock_platform_update_rollout_parent_update();

-- The update-history watermark is part of the immutable planning snapshot.
-- Legacy entries intentionally remain NULL: E3d must fail closed rather than
-- infer that jobs created between an older snapshot and this migration were
-- already observed. Every new entry derives its watermark in PostgreSQL while
-- holding the global admission mutex plus the canonical per-server advisory lock.
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

        -- Acquire the global admission mutex before any parent or server lock.
        -- The transaction-local parent marker below is retained as a structural
        -- one-parent policy, not as trusted concurrency evidence.
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
