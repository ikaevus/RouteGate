-- Separate the administrator's requested protocol preference from the last
-- protocol that was successfully deployed for the account. Client connection
-- material must continue to use active_protocol until an Agent-confirmed apply
-- succeeds, so selecting a protocol cannot immediately cut off working access.

ALTER TABLE vpn_client_profiles
    ADD COLUMN IF NOT EXISTS active_protocol TEXT;

UPDATE vpn_client_profiles cp
SET active_protocol = COALESCE(NULLIF(s.vpn_protocol, 'auto'), 'vless')
FROM vpn_accounts a
LEFT JOIN servers s ON s.id = a.server_id
WHERE cp.vpn_account_id = a.id
  AND cp.active_protocol IS NULL;

UPDATE vpn_client_profiles
SET active_protocol = 'vless'
WHERE active_protocol IS NULL;

ALTER TABLE vpn_client_profiles
    ALTER COLUMN active_protocol SET DEFAULT 'vless',
    ALTER COLUMN active_protocol SET NOT NULL;

ALTER TABLE vpn_client_profiles
    DROP CONSTRAINT IF EXISTS vpn_client_profiles_active_protocol_check;

ALTER TABLE vpn_client_profiles
    ADD CONSTRAINT vpn_client_profiles_active_protocol_check
    CHECK (active_protocol IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'));

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

    -- Activate only preferences that existed when this config version was
    -- rendered. A newer preference remains pending for a subsequent render and
    -- deploy instead of being incorrectly marked active by an older apply.
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
  END IF;

  RETURN NEW;
END;
$$;
