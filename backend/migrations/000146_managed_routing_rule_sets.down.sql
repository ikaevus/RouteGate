DROP TABLE IF EXISTS managed_routing_rule_sets;

ALTER TABLE routing_profiles
    DROP COLUMN IF EXISTS default_action;
