-- Client material (subscriptions, connection links) must follow the last
-- Agent-confirmed apply, not settings that were saved but not yet deployed.
-- Each config version therefore records the client-facing node parameters it
-- deploys, derived from its own rendered config. Subscriptions read the
-- snapshot of servers.active_config_version_id, which only a successful apply
-- advances.
--
-- Versions rendered before this migration are filled in by Manager on start
-- from their stored rendered config (see configs.BackfillClientSettings); SQL
-- cannot derive Reality/WireGuard public keys from the deployed private keys.
ALTER TABLE config_versions
    ADD COLUMN IF NOT EXISTS client_settings JSONB;
