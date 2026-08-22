-- Allow one VPN account to keep several protocols enabled at the same time.
-- desired_enabled is the administrator's requested set; active_enabled is the
-- last Agent-confirmed set. This preserves working access while a new protocol
-- is being prepared or while a removal is pending deployment.

CREATE TABLE IF NOT EXISTS vpn_account_protocols (
    vpn_account_id UUID NOT NULL REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    protocol TEXT NOT NULL,
    desired_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    active_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    PRIMARY KEY (vpn_account_id, protocol),
    CONSTRAINT vpn_account_protocols_protocol_check
        CHECK (protocol IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'))
);

CREATE INDEX IF NOT EXISTS vpn_account_protocols_desired_idx
    ON vpn_account_protocols (vpn_account_id, protocol)
    WHERE desired_enabled;

CREATE INDEX IF NOT EXISTS vpn_account_protocols_active_idx
    ON vpn_account_protocols (vpn_account_id, protocol)
    WHERE active_enabled;

-- Preserve the currently working protocol for every existing account.
INSERT INTO vpn_account_protocols (
    vpn_account_id,
    protocol,
    desired_enabled,
    active_enabled,
    updated_at,
    activated_at
)
SELECT
    cp.vpn_account_id,
    COALESCE(NULLIF(cp.active_protocol, ''), 'vless'),
    TRUE,
    TRUE,
    cp.updated_at,
    now()
FROM vpn_client_profiles cp
ON CONFLICT (vpn_account_id, protocol) DO UPDATE
SET
    desired_enabled = TRUE,
    active_enabled = TRUE,
    activated_at = COALESCE(vpn_account_protocols.activated_at, EXCLUDED.activated_at);

-- If an administrator already had a different protocol preference pending,
-- preserve it additively instead of replacing the working protocol.
INSERT INTO vpn_account_protocols (
    vpn_account_id,
    protocol,
    desired_enabled,
    active_enabled,
    updated_at
)
SELECT
    cp.vpn_account_id,
    COALESCE(NULLIF(cp.protocol, 'auto'), NULLIF(s.vpn_protocol, 'auto'), 'vless'),
    TRUE,
    FALSE,
    cp.updated_at
FROM vpn_client_profiles cp
JOIN vpn_accounts a ON a.id = cp.vpn_account_id
LEFT JOIN servers s ON s.id = a.server_id
ON CONFLICT (vpn_account_id, protocol) DO UPDATE
SET desired_enabled = TRUE;

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
