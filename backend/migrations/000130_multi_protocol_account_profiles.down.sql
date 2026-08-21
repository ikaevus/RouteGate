DROP TRIGGER IF EXISTS trg_vpn_client_profiles_mark_server_dirty ON vpn_client_profiles;
DROP FUNCTION IF EXISTS routegate_mark_vpn_client_profile_server_dirty_after_update();

ALTER TABLE vpn_client_profiles
    DROP CONSTRAINT IF EXISTS vpn_client_profiles_protocol_check;

ALTER TABLE vpn_client_profiles
    DROP COLUMN IF EXISTS protocol;
