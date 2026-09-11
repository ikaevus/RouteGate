-- Down migration is explicit and fail-safe rather than "best effort".
--
-- The pre-RG-116 schema can only represent at most one active subscription
-- token per VPN account. Once a second device (or a device alongside a
-- legacy account-level token) has an active token - the whole point of this
-- migration - that state cannot be losslessly represented by the old
-- schema. Abort before making any destructive change instead of silently
-- revoking a valid device/user credential to force the rollback through.
--
-- Wrapped in an explicit transaction so the preflight check and every
-- destructive statement below either all apply or none do, regardless of
-- how this file is invoked.
BEGIN;

DO $$
DECLARE
    conflicting_account_id UUID;
    active_token_count INTEGER;
BEGIN
    SELECT vpn_account_id, COUNT(*)
    INTO conflicting_account_id, active_token_count
    FROM vpn_subscription_tokens
    WHERE status = 'active'
    GROUP BY vpn_account_id
    HAVING COUNT(*) > 1
    LIMIT 1;

    IF conflicting_account_id IS NOT NULL THEN
        RAISE EXCEPTION
            'Cannot roll back 000149_vpn_account_devices: VPN account % currently has % active subscription tokens, but the pre-RG-116 schema can represent only one per account. Revoke tokens/devices (see vpn_account_devices / Access & Devices) until every account has at most one active token, then roll back again.',
            conflicting_account_id, active_token_count;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_vpn_subscription_tokens_active_legacy_account;
DROP INDEX IF EXISTS idx_vpn_subscription_tokens_active_device;

CREATE UNIQUE INDEX idx_vpn_subscription_tokens_active_account
    ON vpn_subscription_tokens(vpn_account_id)
    WHERE status = 'active';

ALTER TABLE vpn_subscription_tokens DROP COLUMN IF EXISTS device_id;

DROP TABLE IF EXISTS vpn_account_devices;

COMMIT;
