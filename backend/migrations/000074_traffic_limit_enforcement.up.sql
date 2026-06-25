ALTER TABLE traffic_limits
    ADD COLUMN IF NOT EXISTS limit_exceeded_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS enforcement_status TEXT NOT NULL DEFAULT 'not_enforced',
    ADD COLUMN IF NOT EXISTS enforcement_updated_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'traffic_limits_enforcement_status_check'
    ) THEN
        ALTER TABLE traffic_limits
            ADD CONSTRAINT traffic_limits_enforcement_status_check
            CHECK (enforcement_status IN ('not_enforced', 'within_limit', 'over_limit'));
    END IF;
END $$;

WITH usage_totals AS (
    SELECT
        l.vpn_account_id,
        COALESCE(SUM(e.total_bytes), 0)::bigint AS total_bytes
    FROM traffic_limits l
    LEFT JOIN traffic_usage_events e ON e.vpn_account_id = l.vpn_account_id
    GROUP BY l.vpn_account_id
), next_state AS (
    SELECT
        l.vpn_account_id,
        CASE
            WHEN l.hard_limit_enabled
             AND l.monthly_limit_bytes IS NOT NULL
             AND l.monthly_limit_bytes > 0
             AND u.total_bytes >= l.monthly_limit_bytes
                THEN 'over_limit'
            WHEN l.hard_limit_enabled
             AND l.monthly_limit_bytes IS NOT NULL
             AND l.monthly_limit_bytes > 0
                THEN 'within_limit'
            ELSE 'not_enforced'
        END AS enforcement_status,
        CASE
            WHEN l.hard_limit_enabled
             AND l.monthly_limit_bytes IS NOT NULL
             AND l.monthly_limit_bytes > 0
             AND u.total_bytes >= l.monthly_limit_bytes
                THEN COALESCE(l.limit_exceeded_at, now())
            ELSE NULL
        END AS limit_exceeded_at
    FROM traffic_limits l
    JOIN usage_totals u ON u.vpn_account_id = l.vpn_account_id
)
UPDATE traffic_limits l
SET
    limit_exceeded_at = s.limit_exceeded_at,
    enforcement_status = s.enforcement_status,
    enforcement_updated_at = now(),
    updated_at = now()
FROM next_state s
WHERE l.vpn_account_id = s.vpn_account_id
  AND (
      l.limit_exceeded_at IS DISTINCT FROM s.limit_exceeded_at
      OR l.enforcement_status IS DISTINCT FROM s.enforcement_status
  );
