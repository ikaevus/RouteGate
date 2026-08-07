ALTER TABLE servers
  ADD COLUMN IF NOT EXISTS protocol_updated_at TIMESTAMPTZ;

-- Existing servers historically used updated_at for both configuration changes
-- and Agent liveness. Initialize conservatively from that timestamp so an
-- upgrade never treats an older applied config as current by mistake. After
-- this migration, only protocol mutations advance protocol_updated_at.
UPDATE servers
SET protocol_updated_at = COALESCE(protocol_updated_at, updated_at)
WHERE protocol_updated_at IS NULL;

ALTER TABLE servers
  ALTER COLUMN protocol_updated_at SET DEFAULT now(),
  ALTER COLUMN protocol_updated_at SET NOT NULL;
