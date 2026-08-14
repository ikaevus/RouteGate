CREATE TABLE observability_notification_intents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id UUID NOT NULL REFERENCES observability_alerts(id) ON DELETE CASCADE,
    alert_transition_id UUID NOT NULL UNIQUE REFERENCES observability_alert_transitions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('firing','escalated','resolved')),
    severity TEXT NOT NULL CHECK (severity IN ('warning','critical')),
    rule_key TEXT NOT NULL CHECK (btrim(rule_key) <> ''),
    resource_type TEXT NOT NULL CHECK (btrim(resource_type) <> ''),
    resource_id TEXT NOT NULL CHECK (btrim(resource_id) <> ''),
    reason_code TEXT,
    summary TEXT NOT NULL CHECK (btrim(summary) <> ''),
    claimed_at TIMESTAMPTZ,
    expanded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_observability_notification_intents_claim
    ON observability_notification_intents (claimed_at, created_at, id)
    WHERE expanded_at IS NULL;

CREATE TABLE observability_notification_deliveries (
    delivery_id UUID PRIMARY KEY REFERENCES deliveries(id) ON DELETE CASCADE,
    intent_id UUID NOT NULL REFERENCES observability_notification_intents(id) ON DELETE CASCADE,
    recipient_id UUID REFERENCES delivery_recipients(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_observability_notification_deliveries_recipient
    ON observability_notification_deliveries (intent_id, recipient_id)
    WHERE recipient_id IS NOT NULL;
