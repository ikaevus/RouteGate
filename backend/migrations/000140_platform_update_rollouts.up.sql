CREATE TABLE platform_update_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT platform_update_rollouts_target_version_check CHECK (
        target_version ~ '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$'
    ),
    CONSTRAINT platform_update_rollouts_status_check CHECK (
        status IN ('pending', 'running', 'succeeded', 'failed', 'outcome_unknown')
    ),
    CONSTRAINT platform_update_rollouts_error_code_check CHECK (
        error_code IS NULL OR error_code ~ '^[a-z0-9_]{1,96}$'
    ),
    CONSTRAINT platform_update_rollouts_started_timestamp_check CHECK (
        (status = 'pending' AND started_at IS NULL)
        OR (status IN ('running', 'succeeded', 'failed', 'outcome_unknown') AND started_at IS NOT NULL)
    ),
    CONSTRAINT platform_update_rollouts_completed_timestamp_check CHECK (
        (status IN ('succeeded', 'failed', 'outcome_unknown') AND completed_at IS NOT NULL)
        OR (status IN ('pending', 'running') AND completed_at IS NULL)
    ),
    CONSTRAINT platform_update_rollouts_identity UNIQUE (id, target_version)
);

ALTER TABLE agent_platform_update_jobs
    ADD CONSTRAINT agent_platform_update_jobs_rollout_identity
    UNIQUE (id, server_id, target_version);

CREATE TABLE platform_update_rollout_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id UUID NOT NULL,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,
    target_version TEXT NOT NULL,
    position INTEGER NOT NULL,
    platform_update_job_id UUID,
    status TEXT NOT NULL DEFAULT 'queued',
    blocker_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT platform_update_rollout_entries_rollout_identity_fk FOREIGN KEY (rollout_id, target_version)
        REFERENCES platform_update_rollouts(id, target_version) ON DELETE CASCADE,
    CONSTRAINT platform_update_rollout_entries_job_identity_fk FOREIGN KEY (platform_update_job_id, server_id, target_version)
        REFERENCES agent_platform_update_jobs(id, server_id, target_version) ON DELETE RESTRICT,
    CONSTRAINT platform_update_rollout_entries_position_check CHECK (position >= 0),
    CONSTRAINT platform_update_rollout_entries_target_version_check CHECK (
        target_version ~ '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$'
    ),
    CONSTRAINT platform_update_rollout_entries_status_check CHECK (
        status IN ('queued', 'waiting', 'updating', 'healthy', 'failed', 'outcome_unknown', 'skipped')
    ),
    CONSTRAINT platform_update_rollout_entries_blocker_code_check CHECK (
        blocker_code IS NULL OR blocker_code ~ '^[a-z0-9_]{1,96}$'
    ),
    CONSTRAINT platform_update_rollout_entries_job_check CHECK (
        (status IN ('queued', 'waiting', 'skipped') AND platform_update_job_id IS NULL)
        OR (status IN ('updating', 'healthy', 'failed', 'outcome_unknown') AND platform_update_job_id IS NOT NULL)
    ),
    CONSTRAINT platform_update_rollout_entries_completed_timestamp_check CHECK (
        (status IN ('healthy', 'failed', 'outcome_unknown', 'skipped') AND completed_at IS NOT NULL)
        OR (status IN ('queued', 'waiting', 'updating') AND completed_at IS NULL)
    ),
    CONSTRAINT platform_update_rollout_entries_unique_position UNIQUE (rollout_id, position),
    CONSTRAINT platform_update_rollout_entries_unique_server UNIQUE (rollout_id, server_id),
    CONSTRAINT platform_update_rollout_entries_unique_job UNIQUE (platform_update_job_id)
);

CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.target_version IS DISTINCT FROM OLD.target_version THEN
        RAISE EXCEPTION 'platform update rollout target_version is immutable';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status THEN
        IF NOT (
            (OLD.status = 'pending' AND NEW.status IN ('running', 'failed'))
            OR (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed', 'outcome_unknown'))
        ) THEN
            RAISE EXCEPTION 'invalid platform update rollout transition % -> %', OLD.status, NEW.status;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_platform_update_rollouts_transition
BEFORE UPDATE ON platform_update_rollouts
FOR EACH ROW
EXECUTE FUNCTION enforce_platform_update_rollout_transition();

CREATE OR REPLACE FUNCTION enforce_platform_update_rollout_entry_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.rollout_id IS DISTINCT FROM OLD.rollout_id
        OR NEW.server_id IS DISTINCT FROM OLD.server_id
        OR NEW.target_version IS DISTINCT FROM OLD.target_version
        OR NEW.position IS DISTINCT FROM OLD.position THEN
        RAISE EXCEPTION 'platform update rollout entry identity is immutable';
    END IF;

    IF OLD.platform_update_job_id IS NOT NULL
        AND NEW.platform_update_job_id IS DISTINCT FROM OLD.platform_update_job_id THEN
        RAISE EXCEPTION 'platform update rollout entry job identity is immutable once bound';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status THEN
        IF NOT (
            (OLD.status = 'queued' AND NEW.status IN ('waiting', 'updating', 'skipped'))
            OR (OLD.status = 'waiting' AND NEW.status IN ('updating', 'skipped'))
            OR (OLD.status = 'updating' AND NEW.status IN ('healthy', 'failed', 'outcome_unknown'))
        ) THEN
            RAISE EXCEPTION 'invalid platform update rollout entry transition % -> %', OLD.status, NEW.status;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_platform_update_rollout_entries_transition
BEFORE UPDATE ON platform_update_rollout_entries
FOR EACH ROW
EXECUTE FUNCTION enforce_platform_update_rollout_entry_transition();

-- Rollout and entry rows are durable mutation-history identities. Deleting a
-- row would release its uniqueness slots and permit delete/reinsert replay with
-- a fresh runnable state or job binding, bypassing the UPDATE transition guards.
-- Retention/archival, if added later, must preserve that identity explicitly.
CREATE OR REPLACE FUNCTION reject_platform_update_rollout_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'platform update rollout history is immutable and cannot be deleted';
END;
$$;

CREATE TRIGGER trg_platform_update_rollouts_no_delete
BEFORE DELETE ON platform_update_rollouts
FOR EACH ROW
EXECUTE FUNCTION reject_platform_update_rollout_delete();

CREATE TRIGGER trg_platform_update_rollout_entries_no_delete
BEFORE DELETE ON platform_update_rollout_entries
FOR EACH ROW
EXECUTE FUNCTION reject_platform_update_rollout_delete();

CREATE UNIQUE INDEX idx_platform_update_rollout_entries_one_updating
    ON platform_update_rollout_entries(rollout_id)
    WHERE status = 'updating';

CREATE INDEX idx_platform_update_rollouts_active
    ON platform_update_rollouts(updated_at, id)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_platform_update_rollout_entries_resume
    ON platform_update_rollout_entries(rollout_id, position)
    WHERE status IN ('queued', 'waiting', 'updating');
