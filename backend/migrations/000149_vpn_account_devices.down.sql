DROP INDEX IF EXISTS idx_vpn_subscription_tokens_active_device;

-- Best-effort restoration: this can fail if more than one device now holds
-- an active token for the same account, which is the intended outcome of
-- this migration and expected once devices beyond the backfilled default
-- exist.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_subscription_tokens_active_account
    ON vpn_subscription_tokens(vpn_account_id)
    WHERE status = 'active';

ALTER TABLE vpn_subscription_tokens DROP COLUMN IF EXISTS device_id;

DROP TABLE IF EXISTS vpn_account_devices;
