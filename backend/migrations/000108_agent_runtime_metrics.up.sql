ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS runtime_load_1 DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS runtime_load_5 DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS runtime_load_15 DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS runtime_logical_cpus INTEGER,
    ADD COLUMN IF NOT EXISTS runtime_collected_at TIMESTAMPTZ;

ALTER TABLE agents
    ADD CONSTRAINT agents_runtime_load_1_check CHECK (runtime_load_1 IS NULL OR runtime_load_1 >= 0),
    ADD CONSTRAINT agents_runtime_load_5_check CHECK (runtime_load_5 IS NULL OR runtime_load_5 >= 0),
    ADD CONSTRAINT agents_runtime_load_15_check CHECK (runtime_load_15 IS NULL OR runtime_load_15 >= 0),
    ADD CONSTRAINT agents_runtime_logical_cpus_check CHECK (runtime_logical_cpus IS NULL OR runtime_logical_cpus > 0);
