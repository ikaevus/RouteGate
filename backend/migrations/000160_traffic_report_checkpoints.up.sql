-- One high-water mark per Agent outbox, independent of raw traffic retention.
CREATE TABLE traffic_report_checkpoints (
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    stream_id uuid NOT NULL,
    sequence bigint NOT NULL DEFAULT 0 CHECK (sequence >= 0),
    payload_hash bytea NOT NULL DEFAULT ''::bytea,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, stream_id)
);
