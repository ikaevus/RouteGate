CREATE TABLE observability_current_health (
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    check_key TEXT NOT NULL,
    component TEXT NOT NULL,
    state TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT true,
    reason_code TEXT,
    summary TEXT,
    recommended_action TEXT,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    observed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (resource_type, resource_id, check_key),
    CONSTRAINT observability_current_health_resource_type_not_blank CHECK (btrim(resource_type) <> ''),
    CONSTRAINT observability_current_health_resource_id_not_blank CHECK (btrim(resource_id) <> ''),
    CONSTRAINT observability_current_health_check_key_not_blank CHECK (btrim(check_key) <> ''),
    CONSTRAINT observability_current_health_component_not_blank CHECK (btrim(component) <> ''),
    CONSTRAINT observability_current_health_state_check CHECK (
        state IN ('healthy', 'degraded', 'unhealthy', 'unknown')
    ),
    CONSTRAINT observability_current_health_reason_code_not_blank CHECK (
        reason_code IS NULL OR btrim(reason_code) <> ''
    ),
    CONSTRAINT observability_current_health_recommended_action_not_blank CHECK (
        recommended_action IS NULL OR btrim(recommended_action) <> ''
    ),
    CONSTRAINT observability_current_health_evidence_object_check CHECK (
        jsonb_typeof(evidence) = 'object'
    ),
    CONSTRAINT observability_current_health_expiry_check CHECK (
        expires_at IS NULL OR expires_at >= observed_at
    )
);

CREATE INDEX idx_observability_current_health_state
    ON observability_current_health (state, updated_at DESC);

CREATE INDEX idx_observability_current_health_resource
    ON observability_current_health (resource_type, resource_id, updated_at DESC);

CREATE TABLE observability_health_transitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    check_key TEXT NOT NULL,
    component TEXT NOT NULL,
    previous_state TEXT,
    state TEXT NOT NULL,
    reason_code TEXT,
    summary TEXT,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_health_transitions_resource_type_not_blank CHECK (btrim(resource_type) <> ''),
    CONSTRAINT observability_health_transitions_resource_id_not_blank CHECK (btrim(resource_id) <> ''),
    CONSTRAINT observability_health_transitions_check_key_not_blank CHECK (btrim(check_key) <> ''),
    CONSTRAINT observability_health_transitions_component_not_blank CHECK (btrim(component) <> ''),
    CONSTRAINT observability_health_transitions_previous_state_check CHECK (
        previous_state IS NULL OR previous_state IN ('healthy', 'degraded', 'unhealthy', 'unknown')
    ),
    CONSTRAINT observability_health_transitions_state_check CHECK (
        state IN ('healthy', 'degraded', 'unhealthy', 'unknown')
    ),
    CONSTRAINT observability_health_transitions_evidence_object_check CHECK (
        jsonb_typeof(evidence) = 'object'
    )
);

CREATE INDEX idx_observability_health_transitions_resource_history
    ON observability_health_transitions (resource_type, resource_id, observed_at DESC, id DESC);

CREATE INDEX idx_observability_health_transitions_check_history
    ON observability_health_transitions (check_key, observed_at DESC, id DESC);

CREATE TABLE observability_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type TEXT NOT NULL,
    source TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    correlation_id TEXT,
    payload_schema_version INTEGER NOT NULL DEFAULT 1,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_events_event_type_not_blank CHECK (btrim(event_type) <> ''),
    CONSTRAINT observability_events_source_not_blank CHECK (btrim(source) <> ''),
    CONSTRAINT observability_events_resource_type_not_blank CHECK (btrim(resource_type) <> ''),
    CONSTRAINT observability_events_resource_id_not_blank CHECK (btrim(resource_id) <> ''),
    CONSTRAINT observability_events_correlation_id_not_blank CHECK (
        correlation_id IS NULL OR btrim(correlation_id) <> ''
    ),
    CONSTRAINT observability_events_payload_schema_version_check CHECK (payload_schema_version >= 1),
    CONSTRAINT observability_events_payload_object_check CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX idx_observability_events_resource_history
    ON observability_events (resource_type, resource_id, occurred_at DESC, id DESC);

CREATE INDEX idx_observability_events_type_history
    ON observability_events (event_type, occurred_at DESC, id DESC);

CREATE INDEX idx_observability_events_correlation
    ON observability_events (correlation_id, occurred_at DESC)
    WHERE correlation_id IS NOT NULL;

CREATE TABLE observability_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fingerprint TEXT NOT NULL,
    rule_key TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    severity TEXT NOT NULL,
    condition_state TEXT NOT NULL,
    summary TEXT NOT NULL,
    reason_code TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    firing_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    last_evaluated_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_alerts_fingerprint_not_blank CHECK (btrim(fingerprint) <> ''),
    CONSTRAINT observability_alerts_rule_key_not_blank CHECK (btrim(rule_key) <> ''),
    CONSTRAINT observability_alerts_resource_type_not_blank CHECK (btrim(resource_type) <> ''),
    CONSTRAINT observability_alerts_resource_id_not_blank CHECK (btrim(resource_id) <> ''),
    CONSTRAINT observability_alerts_severity_check CHECK (severity IN ('warning', 'critical')),
    CONSTRAINT observability_alerts_condition_state_check CHECK (
        condition_state IN ('pending', 'firing', 'resolved')
    ),
    CONSTRAINT observability_alerts_summary_not_blank CHECK (btrim(summary) <> ''),
    CONSTRAINT observability_alerts_reason_code_not_blank CHECK (
        reason_code IS NULL OR btrim(reason_code) <> ''
    ),
    CONSTRAINT observability_alerts_time_order_check CHECK (
        last_evaluated_at >= started_at
        AND (firing_at IS NULL OR firing_at >= started_at)
        AND (resolved_at IS NULL OR resolved_at >= started_at)
    ),
    CONSTRAINT observability_alerts_lifecycle_timestamps_check CHECK (
        (condition_state = 'pending' AND firing_at IS NULL AND resolved_at IS NULL)
        OR (condition_state = 'firing' AND firing_at IS NOT NULL AND resolved_at IS NULL)
        OR (condition_state = 'resolved' AND resolved_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX idx_observability_alerts_active_fingerprint
    ON observability_alerts (fingerprint)
    WHERE condition_state IN ('pending', 'firing');

CREATE INDEX idx_observability_alerts_active_queue
    ON observability_alerts (severity, started_at, id)
    WHERE condition_state IN ('pending', 'firing');

CREATE INDEX idx_observability_alerts_resource_history
    ON observability_alerts (resource_type, resource_id, started_at DESC, id DESC);

CREATE TABLE observability_alert_transitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id UUID NOT NULL REFERENCES observability_alerts(id) ON DELETE CASCADE,
    from_state TEXT,
    to_state TEXT NOT NULL,
    from_severity TEXT,
    to_severity TEXT NOT NULL,
    reason_code TEXT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_alert_transitions_from_state_check CHECK (
        from_state IS NULL OR from_state IN ('pending', 'firing', 'resolved')
    ),
    CONSTRAINT observability_alert_transitions_to_state_check CHECK (
        to_state IN ('pending', 'firing', 'resolved')
    ),
    CONSTRAINT observability_alert_transitions_from_severity_check CHECK (
        from_severity IS NULL OR from_severity IN ('warning', 'critical')
    ),
    CONSTRAINT observability_alert_transitions_to_severity_check CHECK (
        to_severity IN ('warning', 'critical')
    ),
    CONSTRAINT observability_alert_transitions_reason_code_not_blank CHECK (
        reason_code IS NULL OR btrim(reason_code) <> ''
    )
);

CREATE INDEX idx_observability_alert_transitions_alert_history
    ON observability_alert_transitions (alert_id, occurred_at, id);

CREATE TABLE observability_alert_acknowledgements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id UUID NOT NULL UNIQUE REFERENCES observability_alerts(id) ON DELETE CASCADE,
    acknowledged_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    note TEXT,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT observability_alert_acknowledgements_note_not_blank CHECK (
        note IS NULL OR btrim(note) <> ''
    )
);
