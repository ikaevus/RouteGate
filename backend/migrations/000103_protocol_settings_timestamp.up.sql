ALTER TABLE servers
  ADD COLUMN IF NOT EXISTS protocol_updated_at TIMESTAMPTZ;

UPDATE servers
SET protocol_updated_at = COALESCE(protocol_updated_at, created_at)
WHERE protocol_updated_at IS NULL;

ALTER TABLE servers
  ALTER COLUMN protocol_updated_at SET DEFAULT now(),
  ALTER COLUMN protocol_updated_at SET NOT NULL;
