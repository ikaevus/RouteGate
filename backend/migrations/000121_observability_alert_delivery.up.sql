CREATE TABLE delivery_system_notification_state (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    enabled_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT delivery_system_notification_state_singleton CHECK (id = 1)
);

INSERT INTO delivery_system_notification_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE delivery_system_notifications (
    delivery_id UUID PRIMARY KEY REFERENCES deliveries(id) ON DELETE CASCADE,
    alert_transition_id UUID NOT NULL REFERENCES observability_alert_transitions(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT delivery_system_notifications_transition_delivery_unique
        UNIQUE (alert_transition_id, delivery_id)
);

CREATE INDEX idx_delivery_system_notifications_transition
    ON delivery_system_notifications(alert_transition_id, created_at);
