-- Bind a device-scoped delivery to the specific subscription-token
-- "generation" that was active when it was created, so idempotency can
-- detect token rotation between the original request and a reused
-- Idempotency-Key.
--
-- This is a non-secret reference to a vpn_subscription_tokens row - never
-- the token itself, its hash, or the access URL. It carries no bearer
-- capability: rows in vpn_subscription_tokens are already hash-only, and
-- this column adds nothing recoverable beyond "which row". ON DELETE SET
-- NULL keeps deliveries intact if the referenced token row is ever deleted
-- outright (rows are normally only revoked, not deleted).
ALTER TABLE deliveries
    ADD COLUMN IF NOT EXISTS subscription_token_id UUID REFERENCES vpn_subscription_tokens(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_deliveries_subscription_token_id ON deliveries (subscription_token_id)
    WHERE subscription_token_id IS NOT NULL;
