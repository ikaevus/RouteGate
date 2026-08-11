-- Data-only compatibility cleanup. The legacy runtimeMetrics JSONB block is
-- intentionally not reconstructed on downgrade because dedicated runtime
-- columns remain the canonical storage introduced by migration 000112.
SELECT 1;
