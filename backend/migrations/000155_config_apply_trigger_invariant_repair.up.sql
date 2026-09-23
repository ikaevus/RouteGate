-- Repair the config apply trigger invariants on hosts whose physical schema
-- drifted (for example after a partially destructive restore) while
-- schema_migrations still records 000105, 000131z and 000133 as applied.
--
-- Without config_apply_jobs_mark_version_applied a successful Agent apply never
-- promotes the desired multi-protocol set: newly enabled protocols stay pending
-- forever and the UI reports protocol_set_activation_not_confirmed.
--
-- Function bodies are the canonical definitions from 000105 and 000133.

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

CREATE OR REPLACE FUNCTION routegate_mark_config_version_applied()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
  rendered_at TIMESTAMPTZ;
  target_server UUID;
BEGIN
  IF NEW.action = 'apply' AND NEW.status = 'succeeded' THEN
    UPDATE config_versions
    SET
      status = 'applied',
      applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
    WHERE id = NEW.config_version_id
    RETURNING server_id, created_at INTO target_server, rendered_at;

    -- Keep the legacy primary active protocol in sync for compatibility with
    -- Delivery and existing single-protocol clients.
    UPDATE vpn_client_profiles cp
    SET active_protocol = COALESCE(NULLIF(cp.protocol, 'auto'), NULLIF(s.vpn_protocol, 'auto'), 'vless')
    FROM vpn_accounts a
    JOIN servers s ON s.id = a.server_id
    WHERE cp.vpn_account_id = a.id
      AND a.server_id = target_server
      AND cp.updated_at <= rendered_at
      AND (
        cp.protocol <> 'auto'
        OR COALESCE(s.protocol_updated_at, s.updated_at) <= rendered_at
      );

    -- Promote only protocol-set changes that were part of this rendered config.
    -- Newer edits remain pending and cannot be incorrectly marked active by an
    -- older apply job.
    UPDATE vpn_account_protocols pap
    SET
      active_enabled = pap.desired_enabled,
      activated_at = CASE
        WHEN pap.desired_enabled THEN COALESCE(pap.activated_at, NEW.completed_at, NEW.updated_at, now())
        ELSE NULL
      END
    FROM vpn_accounts a
    WHERE pap.vpn_account_id = a.id
      AND a.server_id = target_server
      AND pap.updated_at <= rendered_at;
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS config_apply_jobs_finalize_lifecycle ON config_apply_jobs;
CREATE TRIGGER config_apply_jobs_finalize_lifecycle
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (
    NEW.action = 'apply'
    AND NEW.status IN ('succeeded', 'failed')
    AND OLD.status IS DISTINCT FROM NEW.status
)
EXECUTE FUNCTION routegate_finalize_config_apply_lifecycle();

DROP TRIGGER IF EXISTS config_apply_jobs_mark_version_applied ON config_apply_jobs;
CREATE TRIGGER config_apply_jobs_mark_version_applied
AFTER UPDATE OF status ON config_apply_jobs
FOR EACH ROW
WHEN (NEW.action = 'apply' AND NEW.status = 'succeeded')
EXECUTE FUNCTION routegate_mark_config_version_applied();

-- Promote protocol sets that the most recent Agent-confirmed apply of each
-- server already covered but that the missing trigger never activated. Only
-- preferences saved no later than the applied version was rendered qualify,
-- exactly as the trigger would have decided.
WITH latest_success AS (
    SELECT DISTINCT ON (j.server_id)
        j.server_id,
        cv.created_at AS rendered_at,
        COALESCE(j.completed_at, j.updated_at) AS completed_at
    FROM config_apply_jobs j
    JOIN config_versions cv ON cv.id = j.config_version_id
    WHERE j.action = 'apply'
      AND j.status = 'succeeded'
    ORDER BY j.server_id, COALESCE(j.completed_at, j.updated_at, j.created_at) DESC, j.created_at DESC
)
UPDATE vpn_account_protocols pap
SET
    active_enabled = pap.desired_enabled,
    activated_at = CASE
        WHEN pap.desired_enabled THEN COALESCE(pap.activated_at, latest_success.completed_at, now())
        ELSE NULL
    END
FROM vpn_accounts a
JOIN latest_success ON latest_success.server_id = a.server_id
WHERE pap.vpn_account_id = a.id
  AND pap.updated_at <= latest_success.rendered_at
  AND pap.active_enabled IS DISTINCT FROM pap.desired_enabled;
