ALTER TABLE agents
    ADD COLUMN credential_generation BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN last_authenticated_heartbeat_at TIMESTAMPTZ,
    ADD COLUMN last_authenticated_heartbeat_generation BIGINT;

ALTER TABLE agents
    ADD CONSTRAINT agents_credential_generation_positive_check CHECK (
        credential_generation > 0
    ),
    ADD CONSTRAINT agents_authenticated_heartbeat_proof_pair_check CHECK (
        (last_authenticated_heartbeat_at IS NULL) =
        (last_authenticated_heartbeat_generation IS NULL)
    ),
    ADD CONSTRAINT agents_authenticated_heartbeat_current_generation_check CHECK (
        last_authenticated_heartbeat_generation IS NULL
        OR last_authenticated_heartbeat_generation = credential_generation
    );

COMMENT ON COLUMN agents.credential_generation IS
    'Monotonic credential/registration generation used to prevent heartbeat proof from surviving Agent credential replacement.';
COMMENT ON COLUMN agents.last_authenticated_heartbeat_at IS
    'Dedicated bearer-authenticated heartbeat evidence; registration and generic liveness writers must not populate this field.';
COMMENT ON COLUMN agents.last_authenticated_heartbeat_generation IS
    'Credential generation that authenticated last_authenticated_heartbeat_at; must match the current generation when present.';

-- E3e extends the E3d rollout-entry state machine with exactly one new forward
-- transition: updating -> healthy. PostgreSQL independently re-proves all
-- durable health predicates at the write boundary so a direct/raw UPDATE cannot
-- manufacture the durable healthy label that unlocks the next persisted node.
-- Protocol bounds intentionally mirror buildinfo for this schema generation
-- (minimum=1, manager=1); a future protocol-policy change must update this
-- database proof together with Manager compatibility policy.
CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_entry_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_server_id TEXT;
    transaction_rollout_id TEXT;
    bound_job_status TEXT;
    bound_job_completed_at TIMESTAMPTZ;
    bound_agent_id UUID;
    current_job_count BIGINT;
    agent_status TEXT;
    agent_version TEXT;
    agent_protocol_version INTEGER;
    credential_generation BIGINT;
    heartbeat_at TIMESTAMPTZ;
    heartbeat_generation BIGINT;
    proof_now TIMESTAMPTZ;
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
            OR (OLD.status = 'updating' AND NEW.status IN ('healthy', 'failed', 'outcome_unknown'))
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

        IF NEW.status = 'healthy' THEN
            IF OLD.platform_update_job_id IS NULL OR OLD.observed_update_job_count IS NULL THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires immutable bound job and history watermark';
            END IF;

            -- The statement-level UPDATE guard already acquired the global
            -- admission mutex before PostgreSQL took this entry row. Reacquire
            -- reentrantly and then follow the E3e server -> Agent proof order.
            PERFORM lock_platform_update_admission_global();
            PERFORM 1
            FROM platform_update_rollouts
            WHERE id = OLD.rollout_id
              AND target_version = OLD.target_version
              AND status = 'running'
            FOR UPDATE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires running parent';
            END IF;
            PERFORM pg_advisory_xact_lock(hashtextextended(OLD.server_id::text, 0));

            SELECT j.status, j.completed_at, j.agent_id
            INTO bound_job_status, bound_job_completed_at, bound_agent_id
            FROM agent_platform_update_jobs j
            WHERE j.id = OLD.platform_update_job_id
              AND j.server_id = OLD.server_id
              AND j.target_version = OLD.target_version
            FOR UPDATE;
            IF NOT FOUND OR bound_job_status <> 'succeeded' OR bound_job_completed_at IS NULL THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires succeeded exact bound job';
            END IF;

            SELECT count(*)
            INTO current_job_count
            FROM agent_platform_update_jobs
            WHERE server_id = OLD.server_id;
            IF current_job_count <> OLD.observed_update_job_count + 1 THEN
                RAISE EXCEPTION 'platform update rollout healthy proof invalidated by intervening update history';
            END IF;

            PERFORM 1
            FROM agent_platform_update_jobs
            WHERE server_id = OLD.server_id
              AND status IN ('pending', 'in_progress', 'mutation_dispatched', 'outcome_unknown')
            LIMIT 1;
            IF FOUND THEN
                RAISE EXCEPTION 'platform update rollout healthy proof blocked by unresolved update outcome';
            END IF;

            SELECT a.status,
                   a.agent_version,
                   a.protocol_version,
                   a.credential_generation,
                   a.last_authenticated_heartbeat_at,
                   a.last_authenticated_heartbeat_generation
            INTO agent_status,
                 agent_version,
                 agent_protocol_version,
                 credential_generation,
                 heartbeat_at,
                 heartbeat_generation
            FROM agents a
            WHERE a.id = bound_agent_id
              AND a.server_id = OLD.server_id
            FOR UPDATE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires current bound Agent';
            END IF;

            -- clock_timestamp() is intentionally captured only after every
            -- potentially blocking proof lock above. Transaction-start time is
            -- not sufficient for heartbeat freshness.
            proof_now := clock_timestamp();
            IF heartbeat_at IS NULL
                OR heartbeat_generation IS NULL
                OR heartbeat_generation <> credential_generation
                OR heartbeat_at <= bound_job_completed_at
                OR heartbeat_at > proof_now
                OR heartbeat_at < proof_now - interval '2 minutes' THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires fresh post-completion authenticated heartbeat';
            END IF;
            IF agent_status <> 'online' OR agent_version <> OLD.target_version THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires online Agent at target version';
            END IF;
            IF agent_protocol_version IS NULL
                OR agent_protocol_version < 1
                OR agent_protocol_version > 1 THEN
                RAISE EXCEPTION 'platform update rollout healthy proof requires Manager-compatible Agent protocol';
            END IF;

            -- The database owns the proof timestamp. Caller-provided timestamps
            -- cannot extend freshness or manufacture causal ordering.
            NEW.completed_at := proof_now;
            NEW.blocker_code := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
