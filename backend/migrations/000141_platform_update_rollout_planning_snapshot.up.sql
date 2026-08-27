ALTER TABLE platform_update_rollout_entries
    ADD COLUMN planning_blockers TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

-- Migration 000140 could already contain durable skipped entries, but those
-- rows predate planning evidence. Give them a bounded fail-closed blocker
-- before validating the new snapshot constraint so an in-place upgrade cannot
-- fail merely because legacy skipped rows exist.
UPDATE platform_update_rollout_entries
SET planning_blockers = ARRAY['update_capability_not_ready']::TEXT[]
WHERE status = 'skipped'
  AND cardinality(planning_blockers) = 0;

ALTER TABLE platform_update_rollout_entries
    ADD CONSTRAINT platform_update_rollout_entries_planning_blockers_check CHECK (
        cardinality(planning_blockers) <= 8
        AND planning_blockers <@ ARRAY[
            'manager_version_mismatch',
            'not_vpn_role',
            'server_disabled',
            'agent_missing',
            'agent_disabled',
            'update_capability_not_ready',
            'active_or_unresolved_update',
            'agent_protocol_incompatible'
        ]::TEXT[]
    ),
    ADD CONSTRAINT platform_update_rollout_entries_planning_snapshot_check CHECK (
        (status = 'queued' AND cardinality(planning_blockers) = 0)
        OR (status = 'skipped' AND cardinality(planning_blockers) > 0)
        OR status IN ('waiting', 'updating', 'healthy', 'failed', 'outcome_unknown')
    );

-- Planning evidence is part of the immutable rollout membership snapshot.
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

        PERFORM 1
        FROM platform_update_rollouts
        WHERE id = NEW.rollout_id
          AND target_version = NEW.target_version
          AND status = 'pending'
        FOR UPDATE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'platform update rollout entries may only be added while parent is pending';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.rollout_id IS DISTINCT FROM OLD.rollout_id
        OR NEW.server_id IS DISTINCT FROM OLD.server_id
        OR NEW.target_version IS DISTINCT FROM OLD.target_version
        OR NEW.position IS DISTINCT FROM OLD.position
        OR NEW.planning_blockers IS DISTINCT FROM OLD.planning_blockers THEN
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
    END IF;
    RETURN NEW;
END;
$$;
