CREATE TABLE IF NOT EXISTS traffic_limits (
    vpn_account_id UUID PRIMARY KEY REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    monthly_limit_bytes BIGINT,
    hard_limit_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    speed_limit_bps BIGINT,
    reset_day SMALLINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT traffic_limits_monthly_limit_bytes_check CHECK (monthly_limit_bytes IS NULL OR monthly_limit_bytes >= 0),
    CONSTRAINT traffic_limits_speed_limit_bps_check CHECK (speed_limit_bps IS NULL OR speed_limit_bps > 0),
    CONSTRAINT traffic_limits_reset_day_check CHECK (reset_day BETWEEN 1 AND 28)
);

CREATE TABLE IF NOT EXISTS traffic_usage_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    vpn_account_id UUID NOT NULL REFERENCES vpn_accounts(id) ON DELETE CASCADE,
    rx_bytes BIGINT NOT NULL DEFAULT 0,
    tx_bytes BIGINT NOT NULL DEFAULT 0,
    total_bytes BIGINT GENERATED ALWAYS AS (rx_bytes + tx_bytes) STORED,
    observed_at TIMESTAMPTZ NOT NULL,
    reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    source TEXT NOT NULL DEFAULT 'agent',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT traffic_usage_events_rx_bytes_check CHECK (rx_bytes >= 0),
    CONSTRAINT traffic_usage_events_tx_bytes_check CHECK (tx_bytes >= 0),
    CONSTRAINT traffic_usage_events_source_check CHECK (source IN ('agent'))
);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_events_account_observed_at
    ON traffic_usage_events(vpn_account_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_events_server_observed_at
    ON traffic_usage_events(server_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_events_agent_reported_at
    ON traffic_usage_events(agent_id, reported_at DESC);
