-- Restore the timestamp-guarded promotion from 000155.
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
