DROP INDEX IF EXISTS idx_vpn_account_node_groups_group_id;
DROP TABLE IF EXISTS vpn_account_node_groups;

DROP INDEX IF EXISTS idx_vpn_account_routing_profiles_profile_id;
DROP TABLE IF EXISTS vpn_account_routing_profiles;

DROP INDEX IF EXISTS idx_node_group_members_server_id;
DROP TABLE IF EXISTS node_group_members;

DROP INDEX IF EXISTS node_groups_name_ci_unique;
DROP TABLE IF EXISTS node_groups;
