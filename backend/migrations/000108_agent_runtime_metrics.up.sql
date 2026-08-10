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

CREATE OR REPLACE FUNCTION routegate_extract_agent_runtime_metrics()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    metrics JSONB;
BEGIN
    metrics := NEW.capabilities -> 'runtimeMetrics';

    NEW.runtime_load_1 := NULL;
    NEW.runtime_load_5 := NULL;
    NEW.runtime_load_15 := NULL;
    NEW.runtime_logical_cpus := NULL;
    NEW.runtime_collected_at := NULL;

    IF metrics IS NOT NULL AND jsonb_typeof(metrics) = 'object' THEN
        BEGIN
            NEW.runtime_load_1 := (metrics ->> 'load1')::double precision;
            NEW.runtime_load_5 := (metrics ->> 'load5')::double precision;
            NEW.runtime_load_15 := (metrics ->> 'load15')::double precision;
            NEW.runtime_logical_cpus := (metrics ->> 'logicalCpus')::integer;
            NEW.runtime_collected_at := (metrics ->> 'collectedAt')::timestamptz;
        EXCEPTION WHEN OTHERS THEN
            NEW.runtime_load_1 := NULL;
            NEW.runtime_load_5 := NULL;
            NEW.runtime_load_15 := NULL;
            NEW.runtime_logical_cpus := NULL;
            NEW.runtime_collected_at := NULL;
        END;
    END IF;

    NEW.capabilities := COALESCE(NEW.capabilities, '{}'::jsonb) - 'runtimeMetrics';
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS agents_extract_runtime_metrics ON agents;
CREATE TRIGGER agents_extract_runtime_metrics
BEFORE INSERT OR UPDATE OF capabilities ON agents
FOR EACH ROW
EXECUTE FUNCTION routegate_extract_agent_runtime_metrics();
