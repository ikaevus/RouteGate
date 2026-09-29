-- Client material (subscriptions, connection links) must follow the last
-- Agent-confirmed apply, not settings that were saved but not yet deployed.
-- Each rendered config version therefore records the client-facing node
-- parameters it was rendered from. Subscriptions read the snapshot of
-- servers.active_config_version_id, which only a successful apply advances.
--
-- Versions rendered before this migration have no snapshot; client material
-- for such a node keeps using the live server settings until the next
-- successful apply records one.
ALTER TABLE config_versions
    ADD COLUMN IF NOT EXISTS client_settings JSONB;
