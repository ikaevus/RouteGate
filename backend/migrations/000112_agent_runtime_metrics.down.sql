DROP TRIGGER IF EXISTS agents_extract_runtime_metrics ON agents;
DROP FUNCTION IF EXISTS routegate_extract_agent_runtime_metrics();

ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_runtime_load_1_check,
    DROP CONSTRAINT IF EXISTS agents_runtime_load_5_check,
    DROP CONSTRAINT IF EXISTS agents_runtime_load_15_check,
    DROP CONSTRAINT IF EXISTS agents_runtime_logical_cpus_check,
    DROP COLUMN IF EXISTS runtime_collected_at,
    DROP COLUMN IF EXISTS runtime_logical_cpus,
    DROP COLUMN IF EXISTS runtime_load_15,
    DROP COLUMN IF EXISTS runtime_load_5,
    DROP COLUMN IF EXISTS runtime_load_1;
