ALTER TABLE agents
    ADD COLUMN credential_generation BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN last_authenticated_heartbeat_at TIMESTAMPTZ,
    ADD COLUMN last_authenticated_heartbeat_generation BIGINT;

ALTER TABLE agents
    ADD CONSTRAINT agents_credential_generation_positive_check CHECK (
        credential_generation > 0
    ),
    ADD CONSTRAINT agents_authenticated_heartbeat_proof_pair_check CHECK (
        (last_authenticated_heartbeat_at IS NULL) =
        (last_authenticated_heartbeat_generation IS NULL)
    ),
    ADD CONSTRAINT agents_authenticated_heartbeat_current_generation_check CHECK (
        last_authenticated_heartbeat_generation IS NULL
        OR last_authenticated_heartbeat_generation = credential_generation
    );

COMMENT ON COLUMN agents.credential_generation IS
    'Monotonic credential/registration generation used to prevent heartbeat proof from surviving Agent credential replacement.';
COMMENT ON COLUMN agents.last_authenticated_heartbeat_at IS
    'Dedicated bearer-authenticated heartbeat evidence; registration and generic liveness writers must not populate this field.';
COMMENT ON COLUMN agents.last_authenticated_heartbeat_generation IS
    'Credential generation that authenticated last_authenticated_heartbeat_at; must match the current generation when present.';
