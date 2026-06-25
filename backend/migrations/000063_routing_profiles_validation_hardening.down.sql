ALTER TABLE routing_profile_rules
    DROP CONSTRAINT IF EXISTS routing_profile_rules_has_matcher,
    DROP CONSTRAINT IF EXISTS routing_profile_rules_priority_max,
    DROP CONSTRAINT IF EXISTS routing_profile_rules_priority_non_negative,
    DROP CONSTRAINT IF EXISTS routing_profile_rules_name_length,
    DROP CONSTRAINT IF EXISTS routing_profile_rules_name_not_blank;

DROP INDEX IF EXISTS routing_profiles_name_ci_unique;

ALTER TABLE routing_profiles
    DROP CONSTRAINT IF EXISTS routing_profiles_description_length,
    DROP CONSTRAINT IF EXISTS routing_profiles_name_length,
    DROP CONSTRAINT IF EXISTS routing_profiles_name_not_blank;
