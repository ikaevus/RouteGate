-- RouteGate PR #499 (applied client settings): checks BEFORE updating Manager.
--
-- Run against the Manager database while it is still on schema 000155.
-- Read only: the whole file runs in one READ ONLY transaction and ends with
-- ROLLBACK. It prints no keys, passwords, secrets or tokens: rendered configs
-- are inspected only for structure, and MTProto is reported as a boolean.
-- It does not reference columns added by migrations 000156-000158.
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

-- Predicts, from the active render, what each active account is served right
-- after the update, with the same rules as the post-update query Q4:
--   * a node's active version yields a snapshot unless it cannot be derived
--     (structural mirror of configs.clientSettingsFromRenderedJSON; such a node
--     keeps serving saved settings unchecked, as before the update);
--   * accounts are checked per account when "vpnAccounts" is present (null
--     counts as empty) and every entry has an id and a protocol or credential;
--   * an account is served its active protocol rows, or else its primary, and
--     every one of them must be deployed for it (MTProto: the node-wide proxy
--     runs). A multi-protocol set is served whole or not at all, so an active
--     row the render did not deploy withholds the whole set.
\echo '== P2. Predicted state after the update, per node'
\echo '   withheld_first_apply: BLOCKING (node never applied); withheld_until_apply: those accounts get'
\echo '   "awaiting apply" instead of a link until the next successful apply of that node'
WITH nodes AS (
  SELECT s.id AS server_id, s.name, s.vpn_protocol, cv.id AS version_id, cv.rendered_config AS rc
  FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
), node_entries AS (
  SELECT n.server_id, e,
         -- protocols the render deployed for the entry (configs.renderedAccountProtocols)
         ARRAY(SELECT DISTINCT x FROM unnest(
           ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END))
           || CASE WHEN COALESCE(e ->> 'vlessUuid', '') <> '' THEN ARRAY['vless'] ELSE '{}' END
           || CASE WHEN COALESCE(e ->> 'wireGuardPublicKey', '') <> '' OR COALESCE(e ->> 'wireGuardAddress', '') <> '' THEN ARRAY['wireguard'] ELSE '{}' END
           || CASE WHEN COALESCE(e ->> 'hysteria2Username', '') <> '' THEN ARRAY['hysteria2'] ELSE '{}' END
           || CASE WHEN COALESCE(e ->> 'shadowsocksUsername', '') <> '' THEN ARRAY['shadowsocks'] ELSE '{}' END) x) AS protocols
  FROM nodes n
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(n.rc -> 'vpnAccounts') = 'array' THEN n.rc -> 'vpnAccounts' ELSE '[]'::jsonb END) e
), node_state AS (
  SELECT n.*,
         n.version_id IS NULL AS awaiting_first_apply,
         -- snapshot derivation (structural mirror of configs.clientSettingsFromRenderedJSON)
         n.version_id IS NOT NULL AND (
           jsonb_typeof(n.rc -> 'vpnAccounts') NOT IN ('array', 'null')
           OR EXISTS (
             SELECT 1 FROM jsonb_array_elements(
               CASE WHEN jsonb_typeof(n.rc -> 'singBox' -> 'inbounds') = 'array' THEN n.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
             WHERE (lower(i ->> 'type') = 'vless'
                    AND (NOT (COALESCE(i ->> 'listen_port', '') ~ '^[0-9]+$' AND (i ->> 'listen_port')::bigint BETWEEN 1 AND 65535)
                         OR (COALESCE((i -> 'tls' -> 'reality' ->> 'enabled')::boolean, FALSE)
                             AND COALESCE(i -> 'tls' -> 'reality' ->> 'private_key', '') !~ '^\s*[A-Za-z0-9_-]{43}\s*$')))
                OR (lower(i ->> 'type') = 'shadowsocks'
                    AND (NOT COALESCE(i ->> 'listen_port', '') ~ '^[0-9]+$' OR (i ->> 'listen_port')::bigint < 1
                         OR COALESCE(i ->> 'method', '') = '' OR btrim(COALESCE(i ->> 'password', '')) = '')))
           OR (btrim(COALESCE(n.rc ->> 'wireGuard', '')) <> ''
               AND NOT (n.rc ->> 'wireGuard' ~ '(?m)^\s*PrivateKey\s*=\s*[A-Za-z0-9+/]{43}=\s*$'
                        AND n.rc ->> 'wireGuard' ~ '(?m)^\s*ListenPort\s*=\s*[0-9]+\s*$'
                        AND n.rc ->> 'wireGuard' ~ '(?m)^\s*Address\s*='))
           OR (btrim(COALESCE(n.rc ->> 'hysteria2', '')) <> ''
               AND NOT (n.rc ->> 'hysteria2' ~ 'domains' AND n.rc ->> 'hysteria2' ~ 'listen'))
           OR (btrim(COALESCE(n.rc ->> 'mtproto', '')) <> ''
               AND NOT (n.rc ->> 'mtproto' ~ '(?m)^secret = "ee[0-9a-f]{68}"$'
                        AND n.rc ->> 'mtproto' ~ '(?m)^bind-to = "0\.0\.0\.0:[0-9]+"$'))
         ) AS no_snapshot,
         -- per-account checks need a listed account set (configs.renderedAccountDeployments)
         n.rc ? 'vpnAccounts' AND NOT EXISTS (
           SELECT 1 FROM node_entries ne
           WHERE ne.server_id = n.server_id AND (COALESCE(ne.e ->> 'id', '') = '' OR cardinality(ne.protocols) = 0)
         ) AS accounts_known,
         COALESCE(n.rc ->> 'mtproto', '') <> '' AS proxy
  FROM nodes n
), accounts AS (
  SELECT ns.name, ns.server_id, a.id, ns.awaiting_first_apply, ns.no_snapshot, ns.accounts_known, ns.proxy,
         ne.protocols AS deployed, ne.e IS NOT NULL AS listed,
         COALESCE(cp.active_protocol, ne.protocols[1], ns.rc -> 'metadata' -> 'vpnCore' ->> 'protocol', ns.vpn_protocol, 'vless') AS raw_primary,
         COALESCE((SELECT array_agg(p.protocol ORDER BY p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = a.id AND p.active_enabled), '{}') AS active_rows
  FROM node_state ns
  JOIN vpn_accounts a ON a.server_id = ns.server_id AND a.status = 'active'
  LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
  LEFT JOIN node_entries ne ON ne.server_id = ns.server_id AND ne.e ->> 'id' = a.id::text
), evaluated AS (
  SELECT ac.*,
         CASE WHEN ac.raw_primary IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
              THEN ac.raw_primary ELSE 'vless' END AS primary_protocol,
         (SELECT array_agg(proto ORDER BY proto)
          FROM unnest(CASE WHEN cardinality(ac.active_rows) > 0 THEN ac.active_rows
                           ELSE ARRAY[CASE WHEN ac.raw_primary IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
                                           THEN ac.raw_primary ELSE 'vless' END] END) proto
          WHERE NOT (CASE WHEN proto = 'mtproto' THEN ac.proxy
                          WHEN NOT ac.accounts_known THEN TRUE
                          ELSE COALESCE(proto = ANY (ac.deployed), FALSE) END)) AS undeployed
  FROM accounts ac
), classified AS (
  SELECT ev.*,
         CASE WHEN awaiting_first_apply THEN 'withheld_first_apply'
              WHEN no_snapshot THEN 'unchecked_no_snapshot'
              WHEN NOT accounts_known AND undeployed IS NULL THEN 'served_compatibility_mode'
              WHEN undeployed IS NULL THEN 'served'
              ELSE 'withheld_until_apply' END AS state_after_update
  FROM evaluated ev
)
SELECT name, state_after_update, count(*) AS active_accounts,
       bool_or(proxy) AS mtproto_proxy_running
FROM classified
GROUP BY name, state_after_update
ORDER BY name, state_after_update;

\echo '== P3. Active accounts that will not be served a link after the update (ids only)'
WITH nodes AS (
  SELECT s.id AS server_id, s.name, s.vpn_protocol, cv.id AS version_id, cv.rendered_config AS rc
  FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
), node_entries AS (
  SELECT n.server_id, e,
         -- protocols the render deployed for the entry (configs.renderedAccountProtocols)
         ARRAY(SELECT DISTINCT x FROM unnest(
           ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN e -> 'protocols' ELSE '[]'::jsonb END))
           || CASE WHEN COALESCE(e ->> 'vlessUuid', '') <> '' THEN ARRAY['vless'] ELSE '{}' END
           || CASE WHEN COALESCE(e ->> 'wireGuardPublicKey', '') <> '' OR COALESCE(e ->> 'wireGuardAddress', '') <> '' THEN ARRAY['wireguard'] ELSE '{}' END
           || CASE WHEN COALESCE(e ->> 'hysteria2Username', '') <> '' THEN ARRAY['hysteria2'] ELSE '{}' END
           || CASE WHEN COALESCE(e ->> 'shadowsocksUsername', '') <> '' THEN ARRAY['shadowsocks'] ELSE '{}' END) x) AS protocols
  FROM nodes n
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(n.rc -> 'vpnAccounts') = 'array' THEN n.rc -> 'vpnAccounts' ELSE '[]'::jsonb END) e
), node_state AS (
  SELECT n.*,
         n.version_id IS NULL AS awaiting_first_apply,
         -- snapshot derivation (structural mirror of configs.clientSettingsFromRenderedJSON)
         n.version_id IS NOT NULL AND (
           jsonb_typeof(n.rc -> 'vpnAccounts') NOT IN ('array', 'null')
           OR EXISTS (
             SELECT 1 FROM jsonb_array_elements(
               CASE WHEN jsonb_typeof(n.rc -> 'singBox' -> 'inbounds') = 'array' THEN n.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
             WHERE (lower(i ->> 'type') = 'vless'
                    AND (NOT (COALESCE(i ->> 'listen_port', '') ~ '^[0-9]+$' AND (i ->> 'listen_port')::bigint BETWEEN 1 AND 65535)
                         OR (COALESCE((i -> 'tls' -> 'reality' ->> 'enabled')::boolean, FALSE)
                             AND COALESCE(i -> 'tls' -> 'reality' ->> 'private_key', '') !~ '^\s*[A-Za-z0-9_-]{43}\s*$')))
                OR (lower(i ->> 'type') = 'shadowsocks'
                    AND (NOT COALESCE(i ->> 'listen_port', '') ~ '^[0-9]+$' OR (i ->> 'listen_port')::bigint < 1
                         OR COALESCE(i ->> 'method', '') = '' OR btrim(COALESCE(i ->> 'password', '')) = '')))
           OR (btrim(COALESCE(n.rc ->> 'wireGuard', '')) <> ''
               AND NOT (n.rc ->> 'wireGuard' ~ '(?m)^\s*PrivateKey\s*=\s*[A-Za-z0-9+/]{43}=\s*$'
                        AND n.rc ->> 'wireGuard' ~ '(?m)^\s*ListenPort\s*=\s*[0-9]+\s*$'
                        AND n.rc ->> 'wireGuard' ~ '(?m)^\s*Address\s*='))
           OR (btrim(COALESCE(n.rc ->> 'hysteria2', '')) <> ''
               AND NOT (n.rc ->> 'hysteria2' ~ 'domains' AND n.rc ->> 'hysteria2' ~ 'listen'))
           OR (btrim(COALESCE(n.rc ->> 'mtproto', '')) <> ''
               AND NOT (n.rc ->> 'mtproto' ~ '(?m)^secret = "ee[0-9a-f]{68}"$'
                        AND n.rc ->> 'mtproto' ~ '(?m)^bind-to = "0\.0\.0\.0:[0-9]+"$'))
         ) AS no_snapshot,
         -- per-account checks need a listed account set (configs.renderedAccountDeployments)
         n.rc ? 'vpnAccounts' AND NOT EXISTS (
           SELECT 1 FROM node_entries ne
           WHERE ne.server_id = n.server_id AND (COALESCE(ne.e ->> 'id', '') = '' OR cardinality(ne.protocols) = 0)
         ) AS accounts_known,
         COALESCE(n.rc ->> 'mtproto', '') <> '' AS proxy
  FROM nodes n
), accounts AS (
  SELECT ns.name, ns.server_id, a.id, ns.awaiting_first_apply, ns.no_snapshot, ns.accounts_known, ns.proxy,
         ne.protocols AS deployed, ne.e IS NOT NULL AS listed,
         COALESCE(cp.active_protocol, ne.protocols[1], ns.rc -> 'metadata' -> 'vpnCore' ->> 'protocol', ns.vpn_protocol, 'vless') AS raw_primary,
         COALESCE((SELECT array_agg(p.protocol ORDER BY p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = a.id AND p.active_enabled), '{}') AS active_rows
  FROM node_state ns
  JOIN vpn_accounts a ON a.server_id = ns.server_id AND a.status = 'active'
  LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
  LEFT JOIN node_entries ne ON ne.server_id = ns.server_id AND ne.e ->> 'id' = a.id::text
), evaluated AS (
  SELECT ac.*,
         CASE WHEN ac.raw_primary IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
              THEN ac.raw_primary ELSE 'vless' END AS primary_protocol,
         (SELECT array_agg(proto ORDER BY proto)
          FROM unnest(CASE WHEN cardinality(ac.active_rows) > 0 THEN ac.active_rows
                           ELSE ARRAY[CASE WHEN ac.raw_primary IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
                                           THEN ac.raw_primary ELSE 'vless' END] END) proto
          WHERE NOT (CASE WHEN proto = 'mtproto' THEN ac.proxy
                          WHEN NOT ac.accounts_known THEN TRUE
                          ELSE COALESCE(proto = ANY (ac.deployed), FALSE) END)) AS undeployed
  FROM accounts ac
), classified AS (
  SELECT ev.*,
         CASE WHEN awaiting_first_apply THEN 'withheld_first_apply'
              WHEN no_snapshot THEN 'unchecked_no_snapshot'
              WHEN NOT accounts_known AND undeployed IS NULL THEN 'served_compatibility_mode'
              WHEN undeployed IS NULL THEN 'served'
              ELSE 'withheld_until_apply' END AS state_after_update
  FROM evaluated ev
)
SELECT name, id AS account_id, primary_protocol, active_rows, listed AS in_active_render,
       undeployed AS not_deployed_by_active_render, state_after_update
FROM classified
WHERE state_after_update IN ('withheld_first_apply', 'withheld_until_apply')
ORDER BY name, account_id;

-- Mirrors the checks Manager runs when it derives a snapshot at start
-- (configs.clientSettingsFromRenderedJSON). A failing version keeps no
-- snapshot; if it is a node's ACTIVE version, that node keeps serving its
-- saved settings without per-account checks, as before the update.
-- WireGuard/Hysteria2/MTProto checks are structural approximations; the
-- definitive result is post-update query Q3 and the Manager start log.
\echo '== P4. Versions whose snapshot cannot be derived (reason codes only)'
\echo '   Review: is_active = t rows (that node gets no applied-settings protection)'
WITH versions AS (
  SELECT cv.id, cv.server_id, cv.version, cv.status,
         COALESCE(s.active_config_version_id = cv.id, FALSE) AS is_active,
         cv.rendered_config AS rc
  FROM config_versions cv LEFT JOIN servers s ON s.id = cv.server_id
), inbound_checks AS (
  SELECT v.id,
         bool_or(lower(i ->> 'type') = 'vless'
                 AND NOT (COALESCE(i ->> 'listen_port', '') ~ '^[0-9]+$'
                          AND (i ->> 'listen_port')::bigint BETWEEN 1 AND 65535)) AS vless_port_invalid,
         bool_or(lower(i ->> 'type') = 'vless'
                 AND COALESCE((i -> 'tls' -> 'reality' ->> 'enabled')::boolean, FALSE)
                 AND COALESCE(i -> 'tls' -> 'reality' ->> 'private_key', '') !~ '^\s*[A-Za-z0-9_-]{43}\s*$') AS reality_key_unparsable,
         bool_or(lower(i ->> 'type') = 'shadowsocks'
                 AND (NOT COALESCE(i ->> 'listen_port', '') ~ '^[0-9]+$' OR (i ->> 'listen_port')::bigint < 1
                      OR COALESCE(i ->> 'method', '') = '' OR btrim(COALESCE(i ->> 'password', '')) = '')) AS shadowsocks_incomplete
  FROM versions v
  CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(v.rc -> 'singBox' -> 'inbounds') = 'array' THEN v.rc -> 'singBox' -> 'inbounds' ELSE '[]'::jsonb END) i
  GROUP BY v.id
), reasons AS (
  SELECT v.server_id, v.version, v.status, v.is_active,
         concat_ws(', ',
           CASE WHEN jsonb_typeof(v.rc) <> 'object' THEN 'rendered_config_not_object' END,
           CASE WHEN jsonb_typeof(v.rc -> 'vpnAccounts') NOT IN ('array', 'null') THEN 'vpn_accounts_not_a_list' END,
           CASE WHEN ic.vless_port_invalid THEN 'vless_listen_port_invalid' END,
           CASE WHEN ic.reality_key_unparsable THEN 'reality_private_key_unparsable' END,
           CASE WHEN ic.shadowsocks_incomplete THEN 'shadowsocks_inbound_incomplete' END,
           CASE WHEN btrim(COALESCE(v.rc ->> 'wireGuard', '')) <> ''
                 AND NOT (v.rc ->> 'wireGuard' ~ '(?m)^\s*PrivateKey\s*=\s*[A-Za-z0-9+/]{43}=\s*$'
                          AND v.rc ->> 'wireGuard' ~ '(?m)^\s*ListenPort\s*=\s*[0-9]+\s*$'
                          AND v.rc ->> 'wireGuard' ~ '(?m)^\s*Address\s*=')
                THEN 'wireguard_section_unparsable' END,
           CASE WHEN btrim(COALESCE(v.rc ->> 'hysteria2', '')) <> ''
                 AND NOT (v.rc ->> 'hysteria2' ~ 'domains' AND v.rc ->> 'hysteria2' ~ 'listen')
                THEN 'hysteria2_section_unparsable' END,
           CASE WHEN btrim(COALESCE(v.rc ->> 'mtproto', '')) <> ''
                 AND NOT (v.rc ->> 'mtproto' ~ '(?m)^secret = "ee[0-9a-f]{68}"$'
                          AND v.rc ->> 'mtproto' ~ '(?m)^bind-to = "0\.0\.0\.0:[0-9]+"$')
                THEN 'mtproto_section_unparsable' END
         ) AS derive_failure,
         CASE
           WHEN NOT v.rc ? 'vpnAccounts' THEN 'no_account_list'
           WHEN EXISTS (
             SELECT 1 FROM jsonb_array_elements(
               CASE WHEN jsonb_typeof(v.rc -> 'vpnAccounts') = 'array' THEN v.rc -> 'vpnAccounts' ELSE '[]'::jsonb END) e
             WHERE COALESCE(e ->> 'id', '') = ''
                OR (COALESCE(e ->> 'vlessUuid', '') = '' AND COALESCE(e ->> 'wireGuardPublicKey', '') = ''
                    AND COALESCE(e ->> 'wireGuardAddress', '') = '' AND COALESCE(e ->> 'hysteria2Username', '') = ''
                    AND COALESCE(e ->> 'shadowsocksUsername', '') = ''
                    AND CASE WHEN jsonb_typeof(e -> 'protocols') = 'array' THEN jsonb_array_length(e -> 'protocols') ELSE 0 END = 0))
             THEN 'account_entry_without_protocol'
         END AS accounts_unknown_reason
  FROM versions v LEFT JOIN inbound_checks ic ON ic.id = v.id
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
       COALESCE(cv.rendered_config ->> 'mtproto', '') <> '' AS active_version_runs_mtproto_proxy,
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
       COALESCE(cv.rendered_config -> 'metadata' -> 'vpnCore' ->> 'protocol', '-') AS applied_node_protocol,
       CASE WHEN cv.id IS NOT NULL
            THEN s.vpn_protocol IS DISTINCT FROM (cv.rendered_config -> 'metadata' -> 'vpnCore' ->> 'protocol') END AS node_switch_pending,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND cp.protocol <> 'auto' AND cp.protocol <> cp.active_protocol) AS explicit_primary_pending,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND cp.protocol = 'auto') AS auto_profiles
FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY s.name;

ROLLBACK;
