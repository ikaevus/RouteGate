-- Migration 000112 introduced dedicated runtime metric columns and a trigger that
-- extracts capabilities.runtimeMetrics on every capabilities write. Some
-- already-upgraded hosts can have schema_migrations=000112 from an earlier
-- production-like validation state while missing the canonical extractor
-- function/trigger. Reassert the canonical database contract here before
-- rewriting legacy rows.

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

-- Rows that existed before the extractor contract was active can still retain
-- the legacy JSONB block. Rewriting only those rows routes them through the
-- canonical trigger: metric values are preserved in dedicated columns and the
-- runtimeMetrics key is removed from durable capabilities.
UPDATE agents
SET capabilities = capabilities
WHERE capabilities ? 'runtimeMetrics';
