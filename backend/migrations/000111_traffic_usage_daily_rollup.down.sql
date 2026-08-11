DROP TRIGGER IF EXISTS traffic_usage_events_rollup_daily ON traffic_usage_events;
DROP FUNCTION IF EXISTS routegate_rollup_traffic_usage_daily();
DROP TABLE IF EXISTS traffic_usage_daily;
