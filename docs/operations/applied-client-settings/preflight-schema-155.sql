-- RouteGate PR #499 (applied client settings): checks BEFORE updating Manager.
--
-- Run against the Manager database while it is still on schema 000155.
-- Read only: the whole file runs in one READ ONLY transaction and ends with
-- ROLLBACK. It prints no keys, passwords, secrets or tokens: rendered configs
-- are inspected only for structure, and MTProto is reported as a boolean.
-- It does not reference columns added by migrations 000156-000158. Values are
-- read with jsonb_typeof guards and without casts that can fail, so malformed
-- rendered configs yield reason codes instead of aborting the script.
--
--   psql "$DATABASE_URL" -X -f preflight-schema-155.sql
\set ON_ERROR_STOP on
\pset null '-'
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;

\echo '== P0. Schema version (expect 000155_*; no 000156+ rows)'
SELECT version AS latest_migration,
       (SELECT count(*) FROM schema_migrations WHERE version >= '000156') AS migrations_156_plus
FROM schema_migrations ORDER BY version DESC LIMIT 1;

\echo '== P1. Nodes with active accounts and whether any version was ever applied'
\echo '   BLOCKING: has_applied_version = f with active_accounts > 0 (all links withheld after update)'
SELECT s.name, s.id AS server_id, s.status, s.vpn_protocol AS saved_protocol,
       s.active_config_version_id IS NOT NULL AS has_applied_version,
       (SELECT count(*) FROM vpn_accounts a WHERE a.server_id = s.id AND a.status = 'active') AS active_accounts
FROM servers s
ORDER BY has_applied_version, s.name;

-- Deployment model (identical in P2/P3 and in the postflight Q4/Q5). It mirrors
-- vpnaccounts.BuildClientConnection, which GET /client-connection, /sub/, the
-- JSON subscription, devices and deliveries share:
--   * nothing is served before a node's first successful apply;
--   * a node whose active version has no snapshot serves saved settings
--     without checks (as before the update);
--   * the primary (vpn_client_profiles.active_protocol; a missing profile is
--     created from the applied version) is built first, then every active
--     protocol row, including rows GetClientProtocolSets seeds from the
--     snapshot on first read; each must be deployed for the account (MTProto:
--     the node-wide proxy runs), otherwise the whole connection is withheld;
--   * per-account checks need a listed account set (compatibility otherwise).
-- It covers the deployment rule only; a link can still be refused for an
-- incomplete setting or an unsupported deployment role, as before.
-- P2 and P3 share one CTE; keep them identical.
\echo '== P2. Predicted state after the update, per node'
\echo '   withheld_first_apply: BLOCKING (node never applied); withheld_until_apply: those accounts get'
\echo '   "awaiting apply" instead of a link until the next successful apply of that node'
WITH rendered_entries AS (
  -- configs.renderedAccountProtocols: listed protocols plus the credentials the
  -- render issued, in Go order (vless, wireguard, hysteria2, shadowsocks, mtproto)
  SELECT s.id AS server_id,
         CASE WHEN jsonb_typeof(e -> 'id') = 'string' THEN e -> 'id' #>> '{}' ELSE '' END AS account_id,
         ARRAY(SELECT x FROM (SELECT DISTINCT x FROM unnest(ARRAY(SELECT CASE WHEN lower(btrim(x #>> '{}')) = 'auto' THEN 'vless' ELSE lower(btrim(x #>> '{}')) END
                  FROM jsonb_array_elements(CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END) x
                  WHERE jsonb_typeof(x) = 'string')
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'vlessUuid') = 'string' THEN e -> 'vlessUuid' #>> '{}' ELSE '' END) <> '' THEN ARRAY['vless'] ELSE '{}' END
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'wireGuardPublicKey') = 'string' THEN e -> 'wireGuardPublicKey' #>> '{}' ELSE '' END) <> '' OR btrim(CASE WHEN jsonb_typeof(e -> 'wireGuardAddress') = 'string' THEN e -> 'wireGuardAddress' #>> '{}' ELSE '' END) <> '' THEN ARRAY['wireguard'] ELSE '{}' END
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'hysteria2Username') = 'string' THEN e -> 'hysteria2Username' #>> '{}' ELSE '' END) <> '' THEN ARRAY['hysteria2'] ELSE '{}' END
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'shadowsocksUsername') = 'string' THEN e -> 'shadowsocksUsername' #>> '{}' ELSE '' END) <> '' THEN ARRAY['shadowsocks'] ELSE '{}' END) x WHERE x IS NOT NULL AND x <> '') d ORDER BY array_position(ARRAY['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'], x) NULLS LAST, x) AS protocols
  FROM servers s
  JOIN config_versions cv ON cv.id = s.active_config_version_id
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(cv.rendered_config -> 'vpnAccounts') = 'array' THEN cv.rendered_config -> 'vpnAccounts' ELSE '[]'::jsonb END) e
  WHERE jsonb_typeof(e) = 'object'
),
snapshot_nodes AS (
  -- what Manager derives at start from each node's active render
  SELECT s.id AS server_id, s.name,
         cv.id IS NULL AS awaiting_first_apply,
         cv.id IS NOT NULL AND concat_ws(', ',
           CASE WHEN jsonb_typeof(cv.rendered_config) IS DISTINCT FROM 'object' THEN 'rendered_config_not_object' END,
           CASE WHEN COALESCE(jsonb_typeof(cv.rendered_config -> 'vpnAccounts'), 'null') NOT IN ('array', 'null')
                  OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'vpnAccounts') = 'array' THEN cv.rendered_config -> 'vpnAccounts' ELSE '[]'::jsonb END) e
                             WHERE CASE WHEN jsonb_typeof(e) <> 'object' THEN TRUE
                                        ELSE EXISTS (SELECT 1 FROM jsonb_each(e) kv
                                                     WHERE kv.key IN ('id', 'displayName', 'status', 'vlessUuid', 'wireGuardPublicKey',
                                                                      'wireGuardAddress', 'hysteria2Username', 'shadowsocksUsername')
                                                       AND jsonb_typeof(kv.value) NOT IN ('string', 'null'))
                                             OR COALESCE(jsonb_typeof(e -> 'protocols'), 'null') NOT IN ('array', 'null')
                                             OR EXISTS (SELECT 1 FROM jsonb_array_elements(
                                                          CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END) p
                                                        WHERE jsonb_typeof(p) <> 'string') END)
                THEN 'vpn_accounts_malformed' END,
           CASE WHEN COALESCE(jsonb_typeof(cv.rendered_config -> 'singBox'), 'null') NOT IN ('object', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds'), 'null') NOT IN ('array', 'null')
                  OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i WHERE jsonb_typeof(i) <> 'object')
                THEN 'singbox_inbounds_malformed' END,
           CASE WHEN COALESCE(jsonb_typeof(cv.rendered_config -> 'wireGuard'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'hysteria2'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'mtproto'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'metadata'), 'null') NOT IN ('object', 'null')
                THEN 'section_type_mismatch' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'vless'
                               AND NOT COALESCE(CASE WHEN jsonb_typeof(i -> 'listen_port') = 'number' THEN trunc((i -> 'listen_port')::numeric) END BETWEEN 1 AND 65535, FALSE))
                THEN 'vless_listen_port_invalid' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'vless'
                               AND COALESCE(i -> 'tls' -> 'reality' -> 'enabled' = 'true'::jsonb, FALSE)
                               AND (CASE WHEN jsonb_typeof(i -> 'tls' -> 'reality' -> 'private_key') = 'string' THEN i -> 'tls' -> 'reality' -> 'private_key' #>> '{}' ELSE '' END) !~ '^\s*[A-Za-z0-9_-]{43}\s*$')
                THEN 'reality_private_key_unparsable' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'shadowsocks'
                               AND (NOT COALESCE(CASE WHEN jsonb_typeof(i -> 'listen_port') = 'number' THEN trunc((i -> 'listen_port')::numeric) END >= 1, FALSE)
                                    OR (CASE WHEN jsonb_typeof(i -> 'method') = 'string' THEN i -> 'method' #>> '{}' ELSE '' END) = '' OR btrim(CASE WHEN jsonb_typeof(i -> 'password') = 'string' THEN i -> 'password' #>> '{}' ELSE '' END) = ''))
                THEN 'shadowsocks_inbound_incomplete' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) <> ''
                 AND NOT ((CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*PrivateKey\s*=\s*[A-Za-z0-9+/]{43}=\s*$'
                          AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*ListenPort\s*=\s*[0-9]{1,5}\s*$'
                          AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*Address\s*=')
                THEN 'wireguard_section_unparsable' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'hysteria2') = 'string' THEN cv.rendered_config -> 'hysteria2' #>> '{}' ELSE '' END) <> '' AND NOT ((CASE WHEN jsonb_typeof(cv.rendered_config -> 'hysteria2') = 'string' THEN cv.rendered_config -> 'hysteria2' #>> '{}' ELSE '' END) ~ 'domains' AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'hysteria2') = 'string' THEN cv.rendered_config -> 'hysteria2' #>> '{}' ELSE '' END) ~ 'listen')
                THEN 'hysteria2_section_unparsable' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) <> ''
                 AND NOT ((CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) ~ '(?n)^secret = "ee[0-9a-f]{68}"$'
                          AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) ~ '(?n)^bind-to = "0\.0\.0\.0:[0-9]{1,5}"$')
                THEN 'mtproto_section_unparsable' END
         ) = '' AS has_snapshot,
         -- configs.renderedAccountDeployments: known when "vpnAccounts" is present
         -- (null = empty) and every entry has an id and a protocol or credential
         cv.rendered_config ? 'vpnAccounts' AND NOT EXISTS (
           SELECT 1 FROM rendered_entries re
           WHERE re.server_id = s.id AND (re.account_id = '' OR cardinality(re.protocols) = 0)) AS accounts_known,
         btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) <> '' AS proxy,
         CASE WHEN jsonb_typeof(cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol') = 'string' THEN cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol' #>> '{}' ELSE '' END AS node_protocol
  FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
),
snapshot_entries AS (
  SELECT server_id, account_id, NULL::text AS primary_protocol, protocols FROM rendered_entries
),
accounts AS (
  SELECT sn.name, sn.server_id, a.id, sn.awaiting_first_apply, sn.has_snapshot, sn.accounts_known, sn.proxy,
         se.account_id IS NOT NULL AS listed,
         COALESCE(se.protocols, '{}') AS deployed,
         cp.vpn_account_id IS NOT NULL AS has_profile,
         -- vpnaccounts.GetActiveClientProtocol; a missing profile is created
         -- from the applied version first (appliedPrimaryProtocolSQL)
         COALESCE(cp.active_protocol,
                  NULLIF(se.primary_protocol, ''),
                  se.protocols[1],
                  CASE WHEN sn.has_snapshot THEN NULLIF(sn.node_protocol, '') END,
                  'vless') AS raw_primary,
         COALESCE((SELECT array_agg(p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = a.id), '{}') AS existing_rows,
         COALESCE((SELECT array_agg(p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = a.id AND p.active_enabled), '{}') AS active_rows
  FROM snapshot_nodes sn
  JOIN vpn_accounts a ON a.server_id = sn.server_id AND a.status = 'active'
  LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
  LEFT JOIN snapshot_entries se ON se.server_id = sn.server_id AND se.account_id = a.id::text
), served AS (
  SELECT ac.*,
         CASE WHEN lower(btrim(ac.raw_primary)) IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
              THEN lower(btrim(ac.raw_primary)) ELSE 'vless' END AS primary_protocol,
         -- rows GetClientProtocolSets seeds (active) before the set is checked
         ARRAY(SELECT s FROM unnest(CASE WHEN ac.has_snapshot AND ac.accounts_known THEN ac.deployed
                                         ELSE ARRAY[COALESCE(NULLIF(ac.raw_primary, ''), 'vless')] END) s
               WHERE NOT s = ANY (ac.existing_rows)) AS seeded_rows
  FROM accounts ac
), evaluated AS (
  SELECT sv.*,
         -- the primary is built first, then every active protocol is checked
         ARRAY(SELECT x FROM (SELECT DISTINCT x FROM unnest(sv.active_rows || sv.seeded_rows || ARRAY[sv.primary_protocol]) x WHERE x IS NOT NULL AND x <> '') d ORDER BY array_position(ARRAY['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'], x) NULLS LAST, x) AS checked,
         ARRAY(SELECT proto FROM unnest(ARRAY(SELECT x FROM (SELECT DISTINCT x FROM unnest(sv.active_rows || sv.seeded_rows || ARRAY[sv.primary_protocol]) x WHERE x IS NOT NULL AND x <> '') d ORDER BY array_position(ARRAY['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'], x) NULLS LAST, x)) proto
               WHERE NOT (CASE WHEN NOT sv.has_snapshot THEN TRUE
                               WHEN proto = 'mtproto' THEN sv.proxy
                               WHEN NOT sv.accounts_known THEN TRUE
                               ELSE proto = ANY (sv.deployed) END)) AS undeployed
  FROM served sv
), classified AS (
  SELECT ev.*,
         CASE WHEN awaiting_first_apply THEN 'withheld_first_apply'
              WHEN NOT has_snapshot THEN 'unchecked_no_snapshot'
              WHEN cardinality(undeployed) = 0 AND NOT accounts_known THEN 'served_compatibility_mode'
              WHEN cardinality(undeployed) = 0 THEN 'served'
              ELSE 'withheld_until_apply' END AS state
  FROM evaluated ev
)
SELECT name, state AS state_after_update, count(*) AS active_accounts,
       bool_or(proxy) AS mtproto_proxy_running
FROM classified
GROUP BY name, state
ORDER BY name, state;

\echo '== P3. Active accounts that will not be served a link after the update (ids only)'
\echo '   BLOCKING: in_active_render = t (the account loses protocols that work today)'
WITH rendered_entries AS (
  -- configs.renderedAccountProtocols: listed protocols plus the credentials the
  -- render issued, in Go order (vless, wireguard, hysteria2, shadowsocks, mtproto)
  SELECT s.id AS server_id,
         CASE WHEN jsonb_typeof(e -> 'id') = 'string' THEN e -> 'id' #>> '{}' ELSE '' END AS account_id,
         ARRAY(SELECT x FROM (SELECT DISTINCT x FROM unnest(ARRAY(SELECT CASE WHEN lower(btrim(x #>> '{}')) = 'auto' THEN 'vless' ELSE lower(btrim(x #>> '{}')) END
                  FROM jsonb_array_elements(CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END) x
                  WHERE jsonb_typeof(x) = 'string')
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'vlessUuid') = 'string' THEN e -> 'vlessUuid' #>> '{}' ELSE '' END) <> '' THEN ARRAY['vless'] ELSE '{}' END
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'wireGuardPublicKey') = 'string' THEN e -> 'wireGuardPublicKey' #>> '{}' ELSE '' END) <> '' OR btrim(CASE WHEN jsonb_typeof(e -> 'wireGuardAddress') = 'string' THEN e -> 'wireGuardAddress' #>> '{}' ELSE '' END) <> '' THEN ARRAY['wireguard'] ELSE '{}' END
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'hysteria2Username') = 'string' THEN e -> 'hysteria2Username' #>> '{}' ELSE '' END) <> '' THEN ARRAY['hysteria2'] ELSE '{}' END
           || CASE WHEN btrim(CASE WHEN jsonb_typeof(e -> 'shadowsocksUsername') = 'string' THEN e -> 'shadowsocksUsername' #>> '{}' ELSE '' END) <> '' THEN ARRAY['shadowsocks'] ELSE '{}' END) x WHERE x IS NOT NULL AND x <> '') d ORDER BY array_position(ARRAY['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'], x) NULLS LAST, x) AS protocols
  FROM servers s
  JOIN config_versions cv ON cv.id = s.active_config_version_id
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(cv.rendered_config -> 'vpnAccounts') = 'array' THEN cv.rendered_config -> 'vpnAccounts' ELSE '[]'::jsonb END) e
  WHERE jsonb_typeof(e) = 'object'
),
snapshot_nodes AS (
  -- what Manager derives at start from each node's active render
  SELECT s.id AS server_id, s.name,
         cv.id IS NULL AS awaiting_first_apply,
         cv.id IS NOT NULL AND concat_ws(', ',
           CASE WHEN jsonb_typeof(cv.rendered_config) IS DISTINCT FROM 'object' THEN 'rendered_config_not_object' END,
           CASE WHEN COALESCE(jsonb_typeof(cv.rendered_config -> 'vpnAccounts'), 'null') NOT IN ('array', 'null')
                  OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'vpnAccounts') = 'array' THEN cv.rendered_config -> 'vpnAccounts' ELSE '[]'::jsonb END) e
                             WHERE CASE WHEN jsonb_typeof(e) <> 'object' THEN TRUE
                                        ELSE EXISTS (SELECT 1 FROM jsonb_each(e) kv
                                                     WHERE kv.key IN ('id', 'displayName', 'status', 'vlessUuid', 'wireGuardPublicKey',
                                                                      'wireGuardAddress', 'hysteria2Username', 'shadowsocksUsername')
                                                       AND jsonb_typeof(kv.value) NOT IN ('string', 'null'))
                                             OR COALESCE(jsonb_typeof(e -> 'protocols'), 'null') NOT IN ('array', 'null')
                                             OR EXISTS (SELECT 1 FROM jsonb_array_elements(
                                                          CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END) p
                                                        WHERE jsonb_typeof(p) <> 'string') END)
                THEN 'vpn_accounts_malformed' END,
           CASE WHEN COALESCE(jsonb_typeof(cv.rendered_config -> 'singBox'), 'null') NOT IN ('object', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds'), 'null') NOT IN ('array', 'null')
                  OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i WHERE jsonb_typeof(i) <> 'object')
                THEN 'singbox_inbounds_malformed' END,
           CASE WHEN COALESCE(jsonb_typeof(cv.rendered_config -> 'wireGuard'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'hysteria2'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'mtproto'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(cv.rendered_config -> 'metadata'), 'null') NOT IN ('object', 'null')
                THEN 'section_type_mismatch' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'vless'
                               AND NOT COALESCE(CASE WHEN jsonb_typeof(i -> 'listen_port') = 'number' THEN trunc((i -> 'listen_port')::numeric) END BETWEEN 1 AND 65535, FALSE))
                THEN 'vless_listen_port_invalid' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'vless'
                               AND COALESCE(i -> 'tls' -> 'reality' -> 'enabled' = 'true'::jsonb, FALSE)
                               AND (CASE WHEN jsonb_typeof(i -> 'tls' -> 'reality' -> 'private_key') = 'string' THEN i -> 'tls' -> 'reality' -> 'private_key' #>> '{}' ELSE '' END) !~ '^\s*[A-Za-z0-9_-]{43}\s*$')
                THEN 'reality_private_key_unparsable' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(cv.rendered_config -> 'singBox' -> 'inbounds') = 'array' THEN cv.rendered_config -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'shadowsocks'
                               AND (NOT COALESCE(CASE WHEN jsonb_typeof(i -> 'listen_port') = 'number' THEN trunc((i -> 'listen_port')::numeric) END >= 1, FALSE)
                                    OR (CASE WHEN jsonb_typeof(i -> 'method') = 'string' THEN i -> 'method' #>> '{}' ELSE '' END) = '' OR btrim(CASE WHEN jsonb_typeof(i -> 'password') = 'string' THEN i -> 'password' #>> '{}' ELSE '' END) = ''))
                THEN 'shadowsocks_inbound_incomplete' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) <> ''
                 AND NOT ((CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*PrivateKey\s*=\s*[A-Za-z0-9+/]{43}=\s*$'
                          AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*ListenPort\s*=\s*[0-9]{1,5}\s*$'
                          AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'wireGuard') = 'string' THEN cv.rendered_config -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*Address\s*=')
                THEN 'wireguard_section_unparsable' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'hysteria2') = 'string' THEN cv.rendered_config -> 'hysteria2' #>> '{}' ELSE '' END) <> '' AND NOT ((CASE WHEN jsonb_typeof(cv.rendered_config -> 'hysteria2') = 'string' THEN cv.rendered_config -> 'hysteria2' #>> '{}' ELSE '' END) ~ 'domains' AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'hysteria2') = 'string' THEN cv.rendered_config -> 'hysteria2' #>> '{}' ELSE '' END) ~ 'listen')
                THEN 'hysteria2_section_unparsable' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) <> ''
                 AND NOT ((CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) ~ '(?n)^secret = "ee[0-9a-f]{68}"$'
                          AND (CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) ~ '(?n)^bind-to = "0\.0\.0\.0:[0-9]{1,5}"$')
                THEN 'mtproto_section_unparsable' END
         ) = '' AS has_snapshot,
         -- configs.renderedAccountDeployments: known when "vpnAccounts" is present
         -- (null = empty) and every entry has an id and a protocol or credential
         cv.rendered_config ? 'vpnAccounts' AND NOT EXISTS (
           SELECT 1 FROM rendered_entries re
           WHERE re.server_id = s.id AND (re.account_id = '' OR cardinality(re.protocols) = 0)) AS accounts_known,
         btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) <> '' AS proxy,
         CASE WHEN jsonb_typeof(cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol') = 'string' THEN cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol' #>> '{}' ELSE '' END AS node_protocol
  FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
),
snapshot_entries AS (
  SELECT server_id, account_id, NULL::text AS primary_protocol, protocols FROM rendered_entries
),
accounts AS (
  SELECT sn.name, sn.server_id, a.id, sn.awaiting_first_apply, sn.has_snapshot, sn.accounts_known, sn.proxy,
         se.account_id IS NOT NULL AS listed,
         COALESCE(se.protocols, '{}') AS deployed,
         cp.vpn_account_id IS NOT NULL AS has_profile,
         -- vpnaccounts.GetActiveClientProtocol; a missing profile is created
         -- from the applied version first (appliedPrimaryProtocolSQL)
         COALESCE(cp.active_protocol,
                  NULLIF(se.primary_protocol, ''),
                  se.protocols[1],
                  CASE WHEN sn.has_snapshot THEN NULLIF(sn.node_protocol, '') END,
                  'vless') AS raw_primary,
         COALESCE((SELECT array_agg(p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = a.id), '{}') AS existing_rows,
         COALESCE((SELECT array_agg(p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = a.id AND p.active_enabled), '{}') AS active_rows
  FROM snapshot_nodes sn
  JOIN vpn_accounts a ON a.server_id = sn.server_id AND a.status = 'active'
  LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
  LEFT JOIN snapshot_entries se ON se.server_id = sn.server_id AND se.account_id = a.id::text
), served AS (
  SELECT ac.*,
         CASE WHEN lower(btrim(ac.raw_primary)) IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
              THEN lower(btrim(ac.raw_primary)) ELSE 'vless' END AS primary_protocol,
         -- rows GetClientProtocolSets seeds (active) before the set is checked
         ARRAY(SELECT s FROM unnest(CASE WHEN ac.has_snapshot AND ac.accounts_known THEN ac.deployed
                                         ELSE ARRAY[COALESCE(NULLIF(ac.raw_primary, ''), 'vless')] END) s
               WHERE NOT s = ANY (ac.existing_rows)) AS seeded_rows
  FROM accounts ac
), evaluated AS (
  SELECT sv.*,
         -- the primary is built first, then every active protocol is checked
         ARRAY(SELECT x FROM (SELECT DISTINCT x FROM unnest(sv.active_rows || sv.seeded_rows || ARRAY[sv.primary_protocol]) x WHERE x IS NOT NULL AND x <> '') d ORDER BY array_position(ARRAY['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'], x) NULLS LAST, x) AS checked,
         ARRAY(SELECT proto FROM unnest(ARRAY(SELECT x FROM (SELECT DISTINCT x FROM unnest(sv.active_rows || sv.seeded_rows || ARRAY[sv.primary_protocol]) x WHERE x IS NOT NULL AND x <> '') d ORDER BY array_position(ARRAY['vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto'], x) NULLS LAST, x)) proto
               WHERE NOT (CASE WHEN NOT sv.has_snapshot THEN TRUE
                               WHEN proto = 'mtproto' THEN sv.proxy
                               WHEN NOT sv.accounts_known THEN TRUE
                               ELSE proto = ANY (sv.deployed) END)) AS undeployed
  FROM served sv
), classified AS (
  SELECT ev.*,
         CASE WHEN awaiting_first_apply THEN 'withheld_first_apply'
              WHEN NOT has_snapshot THEN 'unchecked_no_snapshot'
              WHEN cardinality(undeployed) = 0 AND NOT accounts_known THEN 'served_compatibility_mode'
              WHEN cardinality(undeployed) = 0 THEN 'served'
              ELSE 'withheld_until_apply' END AS state
  FROM evaluated ev
)
SELECT name, id AS account_id, primary_protocol, checked AS checked_protocols, listed AS in_active_render,
       undeployed AS not_deployed_by_active_render, state AS state_after_update
FROM classified
WHERE state IN ('withheld_first_apply', 'withheld_until_apply')
ORDER BY name, account_id;

-- Mirrors the checks Manager runs when it derives a snapshot at start
-- (configs.clientSettingsFromRenderedJSON). A failing version keeps no
-- snapshot; if it is a node's ACTIVE version, that node keeps serving its
-- saved settings without per-account checks, as before the update.
-- WireGuard/Hysteria2/MTProto text checks and the JSON type checks are
-- structural approximations; the definitive result is post-update query Q3
-- and the Manager start log.
\echo '== P4. Versions whose snapshot cannot be derived (reason codes only)'
\echo '   Review: is_active = t rows (that node gets no applied-settings protection)'
WITH versions AS (
  SELECT cv.id, cv.server_id, cv.version, cv.status,
         COALESCE(s.active_config_version_id = cv.id, FALSE) AS is_active,
         cv.rendered_config AS rc
  FROM config_versions cv LEFT JOIN servers s ON s.id = cv.server_id
), reasons AS (
  SELECT v.server_id, v.version, v.status, v.is_active,
         concat_ws(', ',
           CASE WHEN jsonb_typeof(v.rc) IS DISTINCT FROM 'object' THEN 'rendered_config_not_object' END,
           CASE WHEN COALESCE(jsonb_typeof(v.rc -> 'vpnAccounts'), 'null') NOT IN ('array', 'null')
                  OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(v.rc -> 'vpnAccounts') = 'array' THEN v.rc -> 'vpnAccounts' ELSE '[]'::jsonb END) e
                             WHERE CASE WHEN jsonb_typeof(e) <> 'object' THEN TRUE
                                        ELSE EXISTS (SELECT 1 FROM jsonb_each(e) kv
                                                     WHERE kv.key IN ('id', 'displayName', 'status', 'vlessUuid', 'wireGuardPublicKey',
                                                                      'wireGuardAddress', 'hysteria2Username', 'shadowsocksUsername')
                                                       AND jsonb_typeof(kv.value) NOT IN ('string', 'null'))
                                             OR COALESCE(jsonb_typeof(e -> 'protocols'), 'null') NOT IN ('array', 'null')
                                             OR EXISTS (SELECT 1 FROM jsonb_array_elements(
                                                          CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END) p
                                                        WHERE jsonb_typeof(p) <> 'string') END)
                THEN 'vpn_accounts_malformed' END,
           CASE WHEN COALESCE(jsonb_typeof(v.rc -> 'singBox'), 'null') NOT IN ('object', 'null')
                  OR COALESCE(jsonb_typeof(v.rc -> 'singBox' -> 'inbounds'), 'null') NOT IN ('array', 'null')
                  OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(v.rc -> 'singBox' -> 'inbounds') = 'array' THEN v.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i WHERE jsonb_typeof(i) <> 'object')
                THEN 'singbox_inbounds_malformed' END,
           CASE WHEN COALESCE(jsonb_typeof(v.rc -> 'wireGuard'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(v.rc -> 'hysteria2'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(v.rc -> 'mtproto'), 'null') NOT IN ('string', 'null')
                  OR COALESCE(jsonb_typeof(v.rc -> 'metadata'), 'null') NOT IN ('object', 'null')
                THEN 'section_type_mismatch' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(v.rc -> 'singBox' -> 'inbounds') = 'array' THEN v.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'vless'
                               AND NOT COALESCE(CASE WHEN jsonb_typeof(i -> 'listen_port') = 'number' THEN trunc((i -> 'listen_port')::numeric) END BETWEEN 1 AND 65535, FALSE))
                THEN 'vless_listen_port_invalid' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(v.rc -> 'singBox' -> 'inbounds') = 'array' THEN v.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'vless'
                               AND COALESCE(i -> 'tls' -> 'reality' -> 'enabled' = 'true'::jsonb, FALSE)
                               AND (CASE WHEN jsonb_typeof(i -> 'tls' -> 'reality' -> 'private_key') = 'string' THEN i -> 'tls' -> 'reality' -> 'private_key' #>> '{}' ELSE '' END) !~ '^\s*[A-Za-z0-9_-]{43}\s*$')
                THEN 'reality_private_key_unparsable' END,
           CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(v.rc -> 'singBox' -> 'inbounds') = 'array' THEN v.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
                             WHERE jsonb_typeof(i) = 'object' AND lower(btrim(CASE WHEN jsonb_typeof(i -> 'type') = 'string' THEN i -> 'type' #>> '{}' ELSE '' END)) = 'shadowsocks'
                               AND (NOT COALESCE(CASE WHEN jsonb_typeof(i -> 'listen_port') = 'number' THEN trunc((i -> 'listen_port')::numeric) END >= 1, FALSE)
                                    OR (CASE WHEN jsonb_typeof(i -> 'method') = 'string' THEN i -> 'method' #>> '{}' ELSE '' END) = '' OR btrim(CASE WHEN jsonb_typeof(i -> 'password') = 'string' THEN i -> 'password' #>> '{}' ELSE '' END) = ''))
                THEN 'shadowsocks_inbound_incomplete' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(v.rc -> 'wireGuard') = 'string' THEN v.rc -> 'wireGuard' #>> '{}' ELSE '' END) <> ''
                 AND NOT ((CASE WHEN jsonb_typeof(v.rc -> 'wireGuard') = 'string' THEN v.rc -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*PrivateKey\s*=\s*[A-Za-z0-9+/]{43}=\s*$'
                          AND (CASE WHEN jsonb_typeof(v.rc -> 'wireGuard') = 'string' THEN v.rc -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*ListenPort\s*=\s*[0-9]{1,5}\s*$'
                          AND (CASE WHEN jsonb_typeof(v.rc -> 'wireGuard') = 'string' THEN v.rc -> 'wireGuard' #>> '{}' ELSE '' END) ~ '(?n)^\s*Address\s*=')
                THEN 'wireguard_section_unparsable' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(v.rc -> 'hysteria2') = 'string' THEN v.rc -> 'hysteria2' #>> '{}' ELSE '' END) <> '' AND NOT ((CASE WHEN jsonb_typeof(v.rc -> 'hysteria2') = 'string' THEN v.rc -> 'hysteria2' #>> '{}' ELSE '' END) ~ 'domains' AND (CASE WHEN jsonb_typeof(v.rc -> 'hysteria2') = 'string' THEN v.rc -> 'hysteria2' #>> '{}' ELSE '' END) ~ 'listen')
                THEN 'hysteria2_section_unparsable' END,
           CASE WHEN btrim(CASE WHEN jsonb_typeof(v.rc -> 'mtproto') = 'string' THEN v.rc -> 'mtproto' #>> '{}' ELSE '' END) <> ''
                 AND NOT ((CASE WHEN jsonb_typeof(v.rc -> 'mtproto') = 'string' THEN v.rc -> 'mtproto' #>> '{}' ELSE '' END) ~ '(?n)^secret = "ee[0-9a-f]{68}"$'
                          AND (CASE WHEN jsonb_typeof(v.rc -> 'mtproto') = 'string' THEN v.rc -> 'mtproto' #>> '{}' ELSE '' END) ~ '(?n)^bind-to = "0\.0\.0\.0:[0-9]{1,5}"$')
                THEN 'mtproto_section_unparsable' END
         ) AS derive_failure,
         CASE
           WHEN NOT v.rc ? 'vpnAccounts' THEN 'no_account_list'
           WHEN EXISTS (
             SELECT 1 FROM jsonb_array_elements(
               CASE WHEN jsonb_typeof(v.rc -> 'vpnAccounts') = 'array' THEN v.rc -> 'vpnAccounts' ELSE '[]'::jsonb END) e
             WHERE jsonb_typeof(e) = 'object'
               AND (btrim(CASE WHEN jsonb_typeof(e -> 'id') = 'string' THEN e -> 'id' #>> '{}' ELSE '' END) = ''
                    OR (btrim(CASE WHEN jsonb_typeof(e -> 'vlessUuid') = 'string' THEN e -> 'vlessUuid' #>> '{}' ELSE '' END) = '' AND btrim(CASE WHEN jsonb_typeof(e -> 'wireGuardPublicKey') = 'string' THEN e -> 'wireGuardPublicKey' #>> '{}' ELSE '' END) = ''
                        AND btrim(CASE WHEN jsonb_typeof(e -> 'wireGuardAddress') = 'string' THEN e -> 'wireGuardAddress' #>> '{}' ELSE '' END) = '' AND btrim(CASE WHEN jsonb_typeof(e -> 'hysteria2Username') = 'string' THEN e -> 'hysteria2Username' #>> '{}' ELSE '' END) = ''
                        AND btrim(CASE WHEN jsonb_typeof(e -> 'shadowsocksUsername') = 'string' THEN e -> 'shadowsocksUsername' #>> '{}' ELSE '' END) = ''
                        AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(
                                          CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END) x
                                        WHERE jsonb_typeof(x) = 'string' AND btrim(x #>> '{}') <> ''))))
             THEN 'account_entry_without_protocol'
         END AS accounts_unknown_reason
  FROM versions v
)
SELECT s.name, r.version, r.status, r.is_active,
       NULLIF(r.derive_failure, '') AS snapshot_derive_failure,
       r.accounts_unknown_reason AS compatibility_mode_reason
FROM reasons r LEFT JOIN servers s ON s.id = r.server_id
WHERE r.derive_failure <> '' OR r.accounts_unknown_reason IS NOT NULL
ORDER BY r.is_active DESC, s.name, r.version;

\echo '== P5. MTProto proxy per node (boolean only) and accounts that use MTProto'
\echo '   The proxy has one node-wide secret: any issued MTProto link works while the proxy runs'
SELECT s.name,
       btrim(CASE WHEN jsonb_typeof(cv.rendered_config -> 'mtproto') = 'string' THEN cv.rendered_config -> 'mtproto' #>> '{}' ELSE '' END) <> '' AS active_version_runs_mtproto_proxy,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND cp.active_protocol = 'mtproto') AS active_mtproto_primary,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_account_protocols p ON p.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND p.protocol = 'mtproto' AND p.active_enabled) AS active_mtproto_rows,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_account_protocols p ON p.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status <> 'active' AND p.protocol = 'mtproto' AND p.active_enabled) AS inactive_accounts_with_mtproto
FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY s.name;

\echo '== P6. Protocol sets per node (migration 000158 keeps every existing row as an explicit choice)'
SELECT s.name, p.protocol,
       count(*) AS total_rows,
       count(*) FILTER (WHERE p.desired_enabled) AS desired_rows,
       count(*) FILTER (WHERE p.active_enabled) AS active_rows,
       count(*) FILTER (WHERE p.desired_enabled AND NOT p.active_enabled) AS pending_enable,
       count(*) FILTER (WHERE NOT p.desired_enabled AND p.active_enabled) AS pending_disable
FROM vpn_account_protocols p
JOIN vpn_accounts a ON a.id = p.vpn_account_id
JOIN servers s ON s.id = a.server_id
GROUP BY s.name, p.protocol
ORDER BY s.name, p.protocol;

\echo '== P7. Pending changes: primary preferences and node protocol switches not applied yet'
SELECT s.name,
       s.vpn_protocol AS saved_node_protocol,
       CASE WHEN cv.id IS NOT NULL THEN CASE WHEN jsonb_typeof(cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol') = 'string' THEN cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol' #>> '{}' ELSE '' END END AS applied_node_protocol,
       CASE WHEN cv.id IS NOT NULL
            THEN s.vpn_protocol IS DISTINCT FROM CASE WHEN jsonb_typeof(cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol') = 'string' THEN cv.rendered_config -> 'metadata' -> 'vpnCore' -> 'protocol' #>> '{}' ELSE '' END END AS node_switch_pending,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND cp.protocol <> 'auto' AND cp.protocol <> cp.active_protocol) AS explicit_primary_pending,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND cp.protocol = 'auto') AS auto_profiles
FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY s.name;

ROLLBACK;
