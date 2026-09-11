DROP INDEX IF EXISTS idx_deliveries_subscription_token_id;
ALTER TABLE deliveries DROP COLUMN IF EXISTS subscription_token_id;
