ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_tcp_listener_ports_distinct_check;

ALTER TABLE servers
    ALTER COLUMN mtproto_port SET DEFAULT 8443;

-- Deliberately keep any ports remapped by the up migration. Re-introducing a
-- known listener collision during rollback would be more dangerous than
-- retaining the safe, explicit port values.
