DROP INDEX IF EXISTS idx_deliveries_device_id;
ALTER TABLE deliveries DROP COLUMN IF EXISTS device_id;
