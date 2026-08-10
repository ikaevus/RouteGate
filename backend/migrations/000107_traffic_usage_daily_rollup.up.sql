CREATE TABLE IF NOT EXISTS traffic_usage_daily (
    usage_date DATE NOT NULL,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    rx_bytes BIGINT NOT NULL DEFAULT 0,
    tx_bytes BIGINT NOT NULL DEFAULT 0,
    total_bytes BIGINT GENERATED ALWAYS AS (rx_bytes + tx_bytes) STORED,
    PRIMARY KEY (server_id, usage_date),
    CONSTRAINT traffic_usage_daily_rx_bytes_check CHECK (rx_bytes >= 0),
    CONSTRAINT traffic_usage_daily_tx_bytes_check CHECK (tx_bytes >= 0)
);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_daily_usage_date
    ON traffic_usage_daily(usage_date DESC);

INSERT INTO traffic_usage_daily (usage_date, server_id, rx_bytes, tx_bytes)
SELECT
    (observed_at AT TIME ZONE 'UTC')::date,
    server_id,
    COALESCE(SUM(rx_bytes), 0)::bigint,
    COALESCE(SUM(tx_bytes), 0)::bigint
FROM traffic_usage_events
GROUP BY (observed_at AT TIME ZONE 'UTC')::date, server_id
ON CONFLICT (server_id, usage_date)
DO UPDATE SET
    rx_bytes = EXCLUDED.rx_bytes,
    tx_bytes = EXCLUDED.tx_bytes;

CREATE OR REPLACE FUNCTION routegate_rollup_traffic_usage_daily()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO traffic_usage_daily (usage_date, server_id, rx_bytes, tx_bytes)
    VALUES (
        (NEW.observed_at AT TIME ZONE 'UTC')::date,
        NEW.server_id,
        NEW.rx_bytes,
        NEW.tx_bytes
    )
    ON CONFLICT (server_id, usage_date)
    DO UPDATE SET
        rx_bytes = traffic_usage_daily.rx_bytes + EXCLUDED.rx_bytes,
        tx_bytes = traffic_usage_daily.tx_bytes + EXCLUDED.tx_bytes;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS traffic_usage_events_rollup_daily ON traffic_usage_events;
CREATE TRIGGER traffic_usage_events_rollup_daily
AFTER INSERT ON traffic_usage_events
FOR EACH ROW
EXECUTE FUNCTION routegate_rollup_traffic_usage_daily();
