DROP INDEX IF EXISTS idx_servers_deployment_role;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_deployment_role_check,
    DROP COLUMN IF EXISTS deployment_role;
