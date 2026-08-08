ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS active_config_version_id UUID;

ALTER TABLE config_versions
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_config_versions_server_pinned_applied
    ON config_versions(server_id, pinned, applied_at DESC, version DESC);

-- Backfill explicit current state from the latest Agent-confirmed successful apply.
WITH latest_success AS (
    SELECT DISTINCT ON (j.server_id)
        j.server_id,
        j.config_version_id
    FROM config_apply_jobs j
    WHERE j.action = 'apply'
      AND j.status = 'succeeded'
    ORDER BY
        j.server_id,
        COALESCE(j.completed_at, j.updated_at, j.created_at) DESC,
        j.created_at DESC
)
UPDATE servers s
SET active_config_version_id = latest_success.config_version_id
FROM latest_success
WHERE s.id = latest_success.server_id;

DROP TRIGGER IF EXISTS config_apply_jobs_mark_version_applied ON config_apply_jobs;
DROP FUNCTION IF EXISTS routegate_mark_config_version_applied();

CREATE OR REPLACE FUNCTION routegate_prune_config_versions(p_server_id UUID)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    active_version_id UUID;
BEGIN
    SELECT s.active_config_version_id
    INTO active_version_id
    FROM servers s
    WHERE s.id = p_server_id;

    WITH ranked_history AS (
        SELECT
            cv.id,
            ROW_NUMBER() OVER (
                ORDER BY
                    COALESCE(
                        (
                            SELECT MAX(COALESCE(j.completed_at, j.updated_at, j.created_at))
                            FROM config_apply_jobs j
                            WHERE j.config_version_id = cv.id
                              AND j.action = 'apply'
                              AND j.status = 'succeeded'
                        ),
                        cv.applied_at,
                        cv.created_at
                    ) DESC,
                    cv.version DESC
            ) AS history_rank
        FROM config_versions cv
        WHERE cv.server_id = p_server_id
          AND cv.applied_at IS NOT NULL
          AND cv.pinned = FALSE
          AND cv.id IS DISTINCT FROM active_version_id
          AND NOT EXISTS (
              SELECT 1
              FROM config_apply_jobs active_job
              WHERE active_job.config_version_id = cv.id
                AND active_job.status IN ('pending', 'in_progress')
          )
    )
    DELETE FROM config_versions cv
    USING ranked_history history
    WHERE cv.id = history.id
      AND history.history_rank > 5;
END;
$$;

CREATE OR REPLACE FUNCTION routegate_prune_config_apply_jobs(p_server_id UUID)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    WITH ranked_terminal AS (
        SELECT
            j.id,
            ROW_NUMBER() OVER (
                ORDER BY
                    COALESCE(j.completed_at, j.updated_at, j.created_at) DESC,
                    j.created_at DESC,
                    j.id DESC
            ) AS terminal_rank
        FROM config_apply_jobs j
        WHERE j.server_id = p_server_id
          AND j.status IN ('succeeded', 'failed')
    )
    DELETE FROM config_apply_jobs j
    USING ranked_terminal history
    WHERE j.id = history.id
      AND history.terminal_rank > 100;
END;
$$;

CREATE OR REPLACE FUNCTION routegate_finalize_config_apply_lifecycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.action = 'apply'
       AND NEW.status = 'succeeded'
       AND OLD.status IS DISTINCT FROM NEW.status THEN
        UPDATE config_versions
        SET
            status = 'applied',
            applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
        WHERE id = NEW.config_version_id;

        UPDATE servers
        SET
            active_config_version_id = NEW.config_version_id,
            updated_at = now()
        WHERE id = NEW.server_id;

        PERFORM routegate_prune_config_versions(NEW.server_id);
    END IF;

    IF NEW.action = 'apply'
       AND NEW.status IN ('succeeded', 'failed')
       AND OLD.status IS DISTINCT FROM NEW.status THEN
        PERFORM routegate_prune_config_apply_jobs(NEW.server_id);
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER config_apply_jobs_finalize_lifecycle
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (
    NEW.action = 'apply'
    AND NEW.status IN ('succeeded', 'failed')
    AND OLD.status IS DISTINCT FROM NEW.status
)
EXECUTE FUNCTION routegate_finalize_config_apply_lifecycle();

-- Apply the bounded policy to existing servers once during migration.
DO $$
DECLARE
    server_record RECORD;
BEGIN
    FOR server_record IN SELECT id FROM servers LOOP
        PERFORM routegate_prune_config_versions(server_record.id);
        PERFORM routegate_prune_config_apply_jobs(server_record.id);
    END LOOP;
END;
$$;
