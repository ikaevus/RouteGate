-- Migration 000112 introduced dedicated runtime metric columns and a trigger that
-- extracts capabilities.runtimeMetrics on every capabilities write. Rows that
-- already existed before 000112 were not rewritten, so they can still retain
-- the legacy JSONB block. Rewriting only those rows routes them through the
-- existing trigger: metric values are preserved in the dedicated columns and
-- runtimeMetrics is removed from capabilities.
UPDATE agents
SET capabilities = capabilities
WHERE capabilities ? 'runtimeMetrics';
