-- A successful apply must make the applied version the source of truth for
-- each account's active protocols. The previous trigger derived them from the
-- *current* preferences guarded by timestamps, so re-applying an older version
-- (a rollback) left clients on the newer version's protocols, and a preference
-- saved after the render could never be reconciled with what was deployed.
--
-- Config versions record, per rendered account, the protocols they deploy and
-- the primary chosen at render time (config_versions.client_settings.accounts,
-- see 000156). When a version lists its accounts (a JSON object, possibly
-- empty), a successful apply sets:
--   * listed accounts: active_enabled exactly to the deployed protocols and
--     active_protocol to the recorded primary;
--   * accounts the version omits (created or activated after the render):
--     no active protocol except MTProto, because the node has none of their
--     credentials.
-- MTProto is never listed: its proxy uses one node-wide secret and does not
-- enumerate accounts. MTProto rows keep the timestamp-guarded promotion and
-- are only active while the applied version runs the MTProto proxy.
-- Versions that do not list accounts keep the previous behaviour entirely.

CREATE OR REPLACE FUNCTION routegate_mark_config_version_applied()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
  rendered_at TIMESTAMPTZ;
  target_server UUID;
  applied_settings JSONB;
  applied_accounts JSONB;
  accounts_known BOOLEAN;
  mtproto_deployed BOOLEAN;
BEGIN
  IF NEW.action = 'apply' AND NEW.status = 'succeeded' THEN
    UPDATE config_versions
    SET
      status = 'applied',
      applied_at = COALESCE(applied_at, NEW.completed_at, NEW.updated_at, now())
    WHERE id = NEW.config_version_id
    RETURNING server_id, created_at, client_settings
    INTO target_server, rendered_at, applied_settings;

    accounts_known := COALESCE(jsonb_typeof(applied_settings -> 'accounts') = 'object', FALSE);
    applied_accounts := CASE WHEN accounts_known THEN applied_settings -> 'accounts' ELSE '{}'::jsonb END;
    mtproto_deployed := COALESCE(applied_settings ->> 'mtprotoSecret', '') <> ''
      AND COALESCE((applied_settings ->> 'mtprotoPort')::int, 0) > 0;

    IF accounts_known THEN
      -- Listed accounts follow the version exactly (MTProto aside).
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

      -- Every non-MTProto row: active exactly when the version deploys it for
      -- the account; omitted accounts therefore lose all of them.
      UPDATE vpn_account_protocols pap
      SET
        active_enabled = COALESCE((applied_accounts -> (a.id::text) -> 'protocols') ? pap.protocol, FALSE),
        activated_at = CASE
          WHEN COALESCE((applied_accounts -> (a.id::text) -> 'protocols') ? pap.protocol, FALSE)
            THEN COALESCE(pap.activated_at, NEW.completed_at, NEW.updated_at, now())
          ELSE NULL
        END
      FROM vpn_accounts a
      WHERE pap.vpn_account_id = a.id
        AND a.server_id = target_server
        AND pap.protocol <> 'mtproto';

      UPDATE vpn_client_profiles cp
      SET active_protocol = COALESCE(
        NULLIF(applied_accounts -> (a.id::text) ->> 'primary', ''),
        CASE
          WHEN (applied_accounts -> (a.id::text) -> 'protocols') ? cp.active_protocol THEN cp.active_protocol
          WHEN cp.active_protocol = 'mtproto' AND mtproto_deployed THEN cp.active_protocol
        END,
        applied_accounts -> (a.id::text) -> 'protocols' ->> 0,
        cp.active_protocol
      )
      FROM vpn_accounts a
      WHERE cp.vpn_account_id = a.id
        AND a.server_id = target_server
        AND applied_accounts ? (a.id::text);
    END IF;

    -- Accounts the version does not list (all accounts when it lists none)
    -- keep the previous timestamp-guarded primary promotion.
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

    -- Timestamp-guarded promotion for rows the per-account data does not
    -- decide: every row when the version lists no accounts, otherwise only
    -- MTProto rows, which additionally require a running MTProto proxy.
    UPDATE vpn_account_protocols pap
    SET
      active_enabled = pap.desired_enabled AND (pap.protocol <> 'mtproto' OR NOT accounts_known OR mtproto_deployed),
      activated_at = CASE
        WHEN pap.desired_enabled AND (pap.protocol <> 'mtproto' OR NOT accounts_known OR mtproto_deployed)
          THEN COALESCE(pap.activated_at, NEW.completed_at, NEW.updated_at, now())
        ELSE NULL
      END
    FROM vpn_accounts a
    WHERE pap.vpn_account_id = a.id
      AND a.server_id = target_server
      AND (NOT accounts_known OR pap.protocol = 'mtproto')
      AND pap.updated_at <= rendered_at;

    -- MTProto access ends when the applied version no longer runs the proxy,
    -- however recently the preference was saved.
    IF accounts_known AND NOT mtproto_deployed THEN
      UPDATE vpn_account_protocols pap
      SET active_enabled = FALSE, activated_at = NULL
      FROM vpn_accounts a
      WHERE pap.vpn_account_id = a.id
        AND a.server_id = target_server
        AND pap.protocol = 'mtproto';
    END IF;
  END IF;

  RETURN NEW;
END;
$$;
