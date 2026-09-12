-- Keep the physical schema change and migration-history update atomic.
-- Production-like rollback may execute this file directly, so this down
-- migration owns its transaction boundary and removes its own history row.
BEGIN;

DROP INDEX IF EXISTS idx_deliveries_subscription_token_id;
ALTER TABLE deliveries DROP COLUMN IF EXISTS subscription_token_id;

DELETE FROM schema_migrations
WHERE version = '000151_delivery_token_generation';

COMMIT;
