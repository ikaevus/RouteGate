-- Introduce a per-device / per-access-instance model for VPN accounts.
--
-- A VPN account may now have multiple devices (Access & Devices), each with
-- its own opaque subscription token, client type, and independent
-- revoke/rotate lifecycle. This does not change the underlying VPN identity
-- (vpn_client_profiles, vpn_account_protocols): a device is an access/delivery
-- credential, not a second set of protocol credentials.

CREATE TABLE vpn_account_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vpn_account_id UUID NOT NULL REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT 'Device',
    client_type TEXT NOT NULL DEFAULT 'generic',
    device_type TEXT NOT NULL DEFAULT 'other',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT vpn_account_devices_status_check CHECK (status IN ('active', 'revoked')),
    CONSTRAINT vpn_account_devices_name_length_check CHECK (char_length(name) BETWEEN 1 AND 100)
);

CREATE INDEX idx_vpn_account_devices_account_id ON vpn_account_devices(vpn_account_id);
CREATE INDEX idx_vpn_account_devices_account_status ON vpn_account_devices(vpn_account_id, status);

ALTER TABLE vpn_subscription_tokens
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES vpn_account_devices(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_vpn_subscription_tokens_device_id ON vpn_subscription_tokens(device_id);

-- Backfill: give every account with an existing active subscription token a
-- "Default device" carrying over its current account-level client/device
-- type, then attach that token to the new device. Existing subscription URLs
-- keep resolving exactly as before; they simply become visible as one device
-- in the new Access & Devices model instead of being invalidated.
--
-- The legacy vpn_client_profiles.client_type vocabulary is wider than the new
-- RG-116 device allow-list (hiddify, v2rayn, v2rayng, generic): it can be
-- v2raytun, v2box, sing-box, other, or an older/unknown value. A backfilled
-- device row must satisfy the same allow-list the device API itself enforces
-- (allowedDeviceClientTypes in device.go), or a simple rename would fail
-- because UpdateDevice revalidates the existing client_type. Retired
-- V2RayTun/V2Box identities are therefore not preserved as a first-class
-- device client here; they normalize to 'generic', matching normalizeClientType
-- in client_capabilities.go. Historical device_type/platform values are
-- normalized the same defensive way, in case any predate the current
-- allow-list.
INSERT INTO vpn_account_devices (vpn_account_id, name, client_type, device_type, created_at, updated_at)
SELECT
    active.vpn_account_id,
    'Default device',
    CASE COALESCE(NULLIF(cp.client_type, ''), 'generic')
        WHEN 'hiddify' THEN 'hiddify'
        WHEN 'v2rayn'  THEN 'v2rayn'
        WHEN 'v2rayng' THEN 'v2rayng'
        ELSE 'generic'
    END,
    CASE COALESCE(NULLIF(cp.device_type, ''), 'other')
        WHEN 'windows' THEN 'windows'
        WHEN 'ios'     THEN 'ios'
        WHEN 'android' THEN 'android'
        WHEN 'macos'   THEN 'macos'
        WHEN 'linux'   THEN 'linux'
        ELSE 'other'
    END,
    active.created_at,
    now()
FROM (
    SELECT vpn_account_id, MIN(created_at) AS created_at
    FROM vpn_subscription_tokens
    WHERE status = 'active'
    GROUP BY vpn_account_id
) active
LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = active.vpn_account_id;

UPDATE vpn_subscription_tokens st
SET device_id = d.id
FROM vpn_account_devices d
WHERE d.vpn_account_id = st.vpn_account_id
  AND st.status = 'active'
  AND st.device_id IS NULL;

-- Replace the single account-wide "one active token" constraint with two
-- narrower DB-level invariants, so the database - not just application-level
-- revoke-then-insert logic - enforces uniqueness for both token families:
--
--   1. one ACTIVE token per non-null device_id (one per device);
--   2. one ACTIVE token with device_id IS NULL per vpn_account_id (the
--      pre-existing account-level/"legacy" subscription-token endpoints,
--      which are still supported and can still create device_id IS NULL
--      tokens going forward).
--
-- A legacy NULL-device token and one or more device-scoped tokens may
-- coexist on the same account: they are validated by different partial
-- indexes below and are intentionally independent credentials. Backfill
-- above already moves every pre-existing active token off device_id = NULL,
-- so no existing row can violate the new legacy-token index.
DROP INDEX IF EXISTS idx_vpn_subscription_tokens_active_account;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_subscription_tokens_active_device
    ON vpn_subscription_tokens(device_id)
    WHERE status = 'active' AND device_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_subscription_tokens_active_legacy_account
    ON vpn_subscription_tokens(vpn_account_id)
    WHERE status = 'active' AND device_id IS NULL;
