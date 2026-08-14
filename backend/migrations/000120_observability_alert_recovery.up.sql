ALTER TABLE observability_alerts
    ADD COLUMN recovery_started_at TIMESTAMPTZ;

ALTER TABLE observability_alerts
    ADD CONSTRAINT observability_alerts_recovery_started_at_check CHECK (
        recovery_started_at IS NULL OR recovery_started_at >= started_at
    );

CREATE INDEX idx_observability_alerts_recovering
    ON observability_alerts (recovery_started_at, id)
    WHERE condition_state IN ('pending', 'firing')
      AND recovery_started_at IS NOT NULL;
