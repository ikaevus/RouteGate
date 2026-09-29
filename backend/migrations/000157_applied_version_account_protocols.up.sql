-- A successful apply must make the applied version the source of truth for
-- each account's active protocols. The previous trigger derived them from the
-- *current* preferences guarded by timestamps, so re-applying an older version
-- (a rollback) left clients on the newer version's protocols, and a preference
-- saved after the render could never be reconciled with what was deployed.
--
-- Config versions record, per rendered account, the protocols they deploy and
-- the primary chosen at render time (config_versions.client_settings.accounts,
-- see 000156). For accounts listed there, the apply now sets active_protocol
-- and active_enabled exactly to that version. Accounts the version does not
-- list, and versions without per-account data, keep the previous
-- timestamp-guarded behaviour. Newer, unapplied preferences never become
-- active because only protocols present in the applied render are marked.

CREATE OR REPLACE FUNCTION routegate_mark_config_version_applied()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
  rendered_at TIMESTAMPTZ;
  target_server UUID;
  applied_accounts JSONB;
BEGIN
  IF NEW.action = 'apply' AND NEW.status = 'succeeded' THEN
    UPDATE config_versions
    SET
      status = 'applied',
      applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
    WHERE id = NEW.config_version_id
    RETURNING server_id, created_at, client_settings -> 'accounts'
    INTO target_server, rendered_at, applied_accounts;

    IF applied_accounts IS NULL OR jsonb_typeof(applied_accounts) <> 'object' THEN
      applied_accounts := '{}'::jsonb;
    END IF;

    -- Accounts deployed by this version: their state follows the version.
    INSERT INTO vpn_account_protocols (
      vpn_account_id, protocol, desired_enabled, active_enabled, updated_at, activated_at
    )
    SELECT a.id, deployed.protocol, TRUE, TRUE, rendered_at,
           COALESCE(NEW.completed_at, NEW.updated_at, now())
    FROM vpn_accounts a
    CROSS JOIN LATERAL jsonb_array_elements_text(applied_accounts -> (a.id::text) -> 'protocols') AS deployed(protocol)
    WHERE a.server_id = target_server
      AND applied_accounts ? (a.id::text)
    ON CONFLICT (vpn_account_id, protocol) DO NOTHING;

    UPDATE vpn_account_protocols pap
    SET
      active_enabled = (applied_accounts -> (a.id::text) -> 'protocols') ? pap.protocol,
      activated_at = CASE
        WHEN (applied_accounts -> (a.id::text) -> 'protocols') ? pap.protocol
          THEN COALESCE(pap.activated_at, NEW.completed_at, NEW.updated_at, now())
        ELSE NULL
      END
    FROM vpn_accounts a
    WHERE pap.vpn_account_id = a.id
      AND a.server_id = target_server
      AND applied_accounts ? (a.id::text);

    UPDATE vpn_client_profiles cp
    SET active_protocol = COALESCE(
      NULLIF(applied_accounts -> (a.id::text) ->> 'primary', ''),
      CASE WHEN (applied_accounts -> (a.id::text) -> 'protocols') ? cp.active_protocol THEN cp.active_protocol END,
      applied_accounts -> (a.id::text) -> 'protocols' ->> 0,
      cp.active_protocol
    )
    FROM vpn_accounts a
    WHERE cp.vpn_account_id = a.id
      AND a.server_id = target_server
      AND applied_accounts ? (a.id::text);

    -- Other accounts keep the previous timestamp-guarded promotion.
    UPDATE vpn_client_profiles cp
    SET active_protocol = COALESCE(NULLIF(cp.protocol, 'auto'), NULLIF(s.vpn_protocol, 'auto'), 'vless')
    FROM vpn_accounts a
    JOIN servers s ON s.id = a.server_id
    WHERE cp.vpn_account_id = a.id
      AND a.server_id = target_server
      AND NOT (applied_accounts ? (a.id::text))
      AND cp.updated_at <= rendered_at
      AND (
        cp.protocol <> 'auto'
        OR COALESCE(s.protocol_updated_at, s.updated_at) <= rendered_at
      );

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
      AND NOT (applied_accounts ? (a.id::text))
      AND pap.updated_at <= rendered_at;
  END IF;

  RETURN NEW;
END;
$$;
