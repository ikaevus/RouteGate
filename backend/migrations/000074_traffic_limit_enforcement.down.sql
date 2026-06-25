ALTER TABLE traffic_limits
    DROP CONSTRAINT IF EXISTS traffic_limits_enforcement_status_check;

ALTER TABLE traffic_limits
    DROP COLUMN IF EXISTS enforcement_updated_at,
    DROP COLUMN IF EXISTS enforcement_status,
    DROP COLUMN IF EXISTS limit_exceeded_at;
