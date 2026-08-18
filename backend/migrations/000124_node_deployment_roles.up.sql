ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS deployment_role TEXT;

-- Every deployment created before RG-114 used the Clean VPS All-in-One path:
-- Manager, PostgreSQL, Web UI, Agent, and VPN Core shared the same host.
UPDATE servers
SET deployment_role = 'hybrid'
WHERE deployment_role IS NULL;

ALTER TABLE servers
    ALTER COLUMN deployment_role SET DEFAULT 'vpn',
    ALTER COLUMN deployment_role SET NOT NULL;

ALTER TABLE servers
    ADD CONSTRAINT servers_deployment_role_check CHECK (
        deployment_role IN ('management', 'vpn', 'hybrid')
    );

CREATE INDEX idx_servers_deployment_role ON servers(deployment_role);
