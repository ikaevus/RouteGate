ALTER TABLE servers
    ALTER COLUMN mtproto_port SET DEFAULT 9443;

-- VLESS/Reality, Shadowsocks and MTProto all bind TCP listeners. Resolve any
-- legacy collisions before enforcing the invariant. Prefer preserving VLESS
-- because it is the primary access path on existing RouteGate installations.
UPDATE servers
SET shadowsocks_port = CASE
    WHEN vless_port <> 8388 THEN 8388
    WHEN vless_port <> 8389 THEN 8389
    ELSE 9388
END
WHERE shadowsocks_port = vless_port;

UPDATE servers
SET mtproto_port = CASE
    WHEN vless_port <> 9443 AND shadowsocks_port <> 9443 THEN 9443
    WHEN vless_port <> 9444 AND shadowsocks_port <> 9444 THEN 9444
    ELSE 10443
END
WHERE mtproto_port = vless_port
   OR mtproto_port = shadowsocks_port;

ALTER TABLE servers
    DROP CONSTRAINT IF EXISTS servers_tcp_listener_ports_distinct_check;

ALTER TABLE servers
    ADD CONSTRAINT servers_tcp_listener_ports_distinct_check
    CHECK (
        vless_port <> shadowsocks_port
        AND vless_port <> mtproto_port
        AND shadowsocks_port <> mtproto_port
    );
