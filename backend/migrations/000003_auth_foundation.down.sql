DROP INDEX IF EXISTS idx_role_permissions_permission_id;
DROP INDEX IF EXISTS idx_user_roles_role_id;
DROP INDEX IF EXISTS idx_auth_sessions_valid;
DROP INDEX IF EXISTS idx_auth_sessions_token_hash;
DROP INDEX IF EXISTS idx_auth_sessions_user_id;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
ALTER TABLE roles
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS built_in,
    DROP COLUMN IF EXISTS description;
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_status_check,
    DROP CONSTRAINT IF EXISTS users_user_type_check,
    DROP COLUMN IF EXISTS last_login_at,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS user_type,
    DROP COLUMN IF EXISTS username;
ALTER TABLE users
    ALTER COLUMN password_hash SET NOT NULL,
    ALTER COLUMN display_name SET NOT NULL;
