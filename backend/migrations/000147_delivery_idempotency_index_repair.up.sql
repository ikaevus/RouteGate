-- Repair historical schema drift in deliveries.
--
-- Repository.Create relies on
--   INSERT ... ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL
-- so every installation must have the matching partial unique index created
-- by migration 000113_delivery_foundation. Some historical hosts are missing
-- it (the index having been dropped or never created outside the normal
-- migration path), which makes every VPN access delivery creation fail with
-- SQLSTATE 42P10 ("there is no unique or exclusion constraint matching the
-- ON CONFLICT specification").

CREATE UNIQUE INDEX IF NOT EXISTS idx_deliveries_idempotency_key
    ON deliveries (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
