ALTER TABLE routing_profiles
    ADD CONSTRAINT routing_profiles_name_not_blank CHECK (btrim(name) <> ''),
    ADD CONSTRAINT routing_profiles_name_length CHECK (char_length(btrim(name)) <= 120),
    ADD CONSTRAINT routing_profiles_description_length CHECK (description IS NULL OR char_length(btrim(description)) <= 1000);

CREATE UNIQUE INDEX routing_profiles_name_ci_unique
    ON routing_profiles (lower(btrim(name)));

ALTER TABLE routing_profile_rules
    ADD CONSTRAINT routing_profile_rules_name_not_blank CHECK (btrim(name) <> ''),
    ADD CONSTRAINT routing_profile_rules_name_length CHECK (char_length(btrim(name)) <= 120),
    ADD CONSTRAINT routing_profile_rules_priority_non_negative CHECK (priority >= 0),
    ADD CONSTRAINT routing_profile_rules_priority_max CHECK (priority <= 1000000),
    ADD CONSTRAINT routing_profile_rules_has_matcher CHECK (
        cardinality(domains) > 0
        OR cardinality(domain_suffixes) > 0
        OR cardinality(domain_keywords) > 0
        OR cardinality(ip_cidrs) > 0
        OR cardinality(geo_sites) > 0
        OR cardinality(geo_ips) > 0
    );
