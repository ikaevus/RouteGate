DROP INDEX IF EXISTS idx_traffic_usage_events_observed_at;

CREATE OR REPLACE FUNCTION routegate_rollup_traffic_usage_daily()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_date DATE;
BEGIN
    IF TG_OP = 'INSERT' THEN
        target_date := (NEW.observed_at AT TIME ZONE 'UTC')::date;

        INSERT INTO traffic_usage_daily (usage_date, server_id, rx_bytes, tx_bytes)
        VALUES (target_date, NEW.server_id, NEW.rx_bytes, NEW.tx_bytes)
        ON CONFLICT (server_id, usage_date)
        DO UPDATE SET
            rx_bytes = traffic_usage_daily.rx_bytes + EXCLUDED.rx_bytes,
            tx_bytes = traffic_usage_daily.tx_bytes + EXCLUDED.tx_bytes;

        RETURN NEW;
    END IF;

    target_date := (OLD.observed_at AT TIME ZONE 'UTC')::date;

    UPDATE traffic_usage_daily
    SET
        rx_bytes = rx_bytes - OLD.rx_bytes,
        tx_bytes = tx_bytes - OLD.tx_bytes
    WHERE server_id = OLD.server_id
      AND usage_date = target_date;

    DELETE FROM traffic_usage_daily
    WHERE server_id = OLD.server_id
      AND usage_date = target_date
      AND rx_bytes = 0
      AND tx_bytes = 0;

    RETURN OLD;
END;
$$;
