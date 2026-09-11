-- Let a delivery (email/Telegram "Send") record which Access & Devices
-- device it was sent for, so the device card can own Send end-to-end.
--
-- This column is metadata only (audit/scoping): it never carries or implies
-- storage of the device's plaintext bearer token. RG-115 subscription
-- tokens remain hash-only in vpn_subscription_tokens; a device-scoped
-- delivery's access material is held only in the delivery worker's
-- in-memory, short-lived material store (see
-- backend/internal/delivery/device_access_material.go), never persisted.
ALTER TABLE deliveries
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES vpn_account_devices(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_deliveries_device_id ON deliveries (device_id)
    WHERE device_id IS NOT NULL;
