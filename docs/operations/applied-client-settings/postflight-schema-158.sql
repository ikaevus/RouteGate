-- RouteGate PR #499 (applied client settings): checks AFTER updating Manager.
--
-- Run against the Manager database once the new Manager has started (schema
-- 000158 applied and snapshots of existing versions backfilled at start).
-- Read only: one READ ONLY transaction ending with ROLLBACK. It prints no
-- keys, passwords, secrets or tokens: snapshots are inspected only for
-- structure, and MTProto is reported as a boolean. Values are read with
-- jsonb_typeof guards and without casts that can fail.
--
--   psql "$DATABASE_URL" -X -f postflight-schema-158.sql
\set ON_ERROR_STOP on
\pset null '-'
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;

\echo '== Q0. Schema version (expect 000158_explicit_account_protocol_preferences and all three new migrations)'
SELECT (SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1) AS latest_migration,
       (SELECT count(*) FROM schema_migrations
         WHERE version IN ('000156_config_version_client_settings',
                           '000157_applied_version_account_protocols',
                           '000158_explicit_account_protocol_preferences')) AS new_migrations_applied;

\echo '== Q1. Migration 000158: desired_explicit column and existing rows kept as explicit'
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_name = 'vpn_account_protocols' AND column_name = 'desired_explicit';

\echo '   Rows present before the update must all be explicit (compare with preflight P6 total_rows)'
SELECT s.name, p.protocol,
       count(*) AS total_rows,
       count(*) FILTER (WHERE p.desired_enabled) AS desired_rows,
       count(*) FILTER (WHERE p.active_enabled) AS active_rows,
       count(*) FILTER (WHERE p.desired_explicit) AS explicit_rows,
       count(*) FILTER (WHERE NOT p.desired_explicit) AS seeded_rows_since_update
FROM vpn_account_protocols p
JOIN vpn_accounts a ON a.id = p.vpn_account_id
JOIN servers s ON s.id = a.server_id
GROUP BY s.name, p.protocol
ORDER BY s.name, p.protocol;

\echo '== Q2. Snapshot of each node''s active version'
\echo '   has_snapshot = f: backfill failed; that node serves saved settings without per-account checks'
SELECT s.name, cv.version AS active_version,
       cv.client_settings IS NOT NULL AS has_snapshot,
       CASE jsonb_typeof(cv.client_settings -> 'accounts')
         WHEN 'object' THEN 'per_account'
         WHEN 'null' THEN 'compatibility'
         ELSE '-' END AS accounts_mode,
       CASE WHEN jsonb_typeof(cv.client_settings -> 'accounts') = 'object'
            THEN (SELECT count(*) FROM jsonb_object_keys(cv.client_settings -> 'accounts')) END AS listed_accounts,
       CASE WHEN jsonb_typeof(cv.client_settings -> 'vpnProtocol') = 'string' THEN cv.client_settings -> 'vpnProtocol' #>> '{}' ELSE '' END AS applied_node_protocol,
       CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoSecret') = 'string' THEN cv.client_settings -> 'mtprotoSecret' #>> '{}' ELSE '' END <> '' AND COALESCE(CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoPort') = 'number' THEN trunc((cv.client_settings -> 'mtprotoPort')::numeric) END > 0, FALSE) AS snapshot_runs_mtproto_proxy
FROM servers s JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY has_snapshot, s.name;

\echo '== Q3. Versions still without a snapshot after the start-up backfill'
SELECT s.name, cv.version, cv.status, COALESCE(s.active_config_version_id = cv.id, FALSE) AS is_active
FROM config_versions cv LEFT JOIN servers s ON s.id = cv.server_id
WHERE cv.client_settings IS NULL
ORDER BY is_active DESC, s.name, cv.version;

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
-- Q4 and Q5 share one CTE with the preflight P2/P3; keep them identical.
\echo '== Q4. What active accounts are served now, per node (compare with preflight P2)'
WITH snapshot_nodes AS (
  -- the snapshot of each node's active version (backfilled at start or rendered)
  SELECT s.id AS server_id, s.name,
         cv.id IS NULL AS awaiting_first_apply,
         cv.client_settings IS NOT NULL AS has_snapshot,
         COALESCE(jsonb_typeof(cv.client_settings -> 'accounts') = 'object', FALSE) AS accounts_known,
         CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoSecret') = 'string' THEN cv.client_settings -> 'mtprotoSecret' #>> '{}' ELSE '' END <> ''
           AND COALESCE(CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoPort') = 'number' THEN trunc((cv.client_settings -> 'mtprotoPort')::numeric) END > 0, FALSE) AS proxy,
         CASE WHEN jsonb_typeof(cv.client_settings -> 'vpnProtocol') = 'string' THEN cv.client_settings -> 'vpnProtocol' #>> '{}' ELSE '' END AS node_protocol
  FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
),
snapshot_entries AS (
  SELECT s.id AS server_id, acc.key AS account_id,
         CASE WHEN jsonb_typeof(acc.value -> 'primary') = 'string' THEN acc.value -> 'primary' #>> '{}' ELSE '' END AS primary_protocol,
         ARRAY(SELECT x #>> '{}' FROM jsonb_array_elements(
                 CASE WHEN jsonb_typeof(acc.value -> 'protocols') = 'array' THEN acc.value -> 'protocols' ELSE '[]'::jsonb END)
                 WITH ORDINALITY AS t(x, n)
               WHERE jsonb_typeof(x) = 'string' ORDER BY n) AS protocols
  FROM servers s
  JOIN config_versions cv ON cv.id = s.active_config_version_id
  CROSS JOIN LATERAL jsonb_each(
    CASE WHEN jsonb_typeof(cv.client_settings -> 'accounts') = 'object' THEN cv.client_settings -> 'accounts' ELSE '{}'::jsonb END) acc
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
SELECT name,
       CASE state WHEN 'served' THEN 'ready'
                  WHEN 'served_compatibility_mode' THEN 'ready'
                  WHEN 'unchecked_no_snapshot' THEN 'served_unchecked_no_snapshot'
                  WHEN 'withheld_until_apply' THEN 'awaiting_apply'
                  ELSE 'awaiting_first_apply' END AS state,
       count(*) AS active_accounts
FROM classified
GROUP BY 1, 2
ORDER BY 1, 2;

\echo '== Q5. Active accounts not served a link now (ids only; compare with preflight P3)'
WITH snapshot_nodes AS (
  -- the snapshot of each node's active version (backfilled at start or rendered)
  SELECT s.id AS server_id, s.name,
         cv.id IS NULL AS awaiting_first_apply,
         cv.client_settings IS NOT NULL AS has_snapshot,
         COALESCE(jsonb_typeof(cv.client_settings -> 'accounts') = 'object', FALSE) AS accounts_known,
         CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoSecret') = 'string' THEN cv.client_settings -> 'mtprotoSecret' #>> '{}' ELSE '' END <> ''
           AND COALESCE(CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoPort') = 'number' THEN trunc((cv.client_settings -> 'mtprotoPort')::numeric) END > 0, FALSE) AS proxy,
         CASE WHEN jsonb_typeof(cv.client_settings -> 'vpnProtocol') = 'string' THEN cv.client_settings -> 'vpnProtocol' #>> '{}' ELSE '' END AS node_protocol
  FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
),
snapshot_entries AS (
  SELECT s.id AS server_id, acc.key AS account_id,
         CASE WHEN jsonb_typeof(acc.value -> 'primary') = 'string' THEN acc.value -> 'primary' #>> '{}' ELSE '' END AS primary_protocol,
         ARRAY(SELECT x #>> '{}' FROM jsonb_array_elements(
                 CASE WHEN jsonb_typeof(acc.value -> 'protocols') = 'array' THEN acc.value -> 'protocols' ELSE '[]'::jsonb END)
                 WITH ORDINALITY AS t(x, n)
               WHERE jsonb_typeof(x) = 'string' ORDER BY n) AS protocols
  FROM servers s
  JOIN config_versions cv ON cv.id = s.active_config_version_id
  CROSS JOIN LATERAL jsonb_each(
    CASE WHEN jsonb_typeof(cv.client_settings -> 'accounts') = 'object' THEN cv.client_settings -> 'accounts' ELSE '{}'::jsonb END) acc
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
SELECT name, id AS account_id, primary_protocol, checked AS checked_protocols,
       CASE state WHEN 'withheld_first_apply' THEN 'awaiting_first_apply' ELSE 'awaiting_apply' END AS state,
       undeployed AS not_deployed_by_active_version
FROM classified
WHERE state IN ('withheld_first_apply', 'withheld_until_apply')
ORDER BY name, account_id;

\echo '== Q6. MTProto per node (boolean only): proxy in the applied snapshot and accounts served through it'
\echo '   The proxy has one node-wide secret: any MTProto link issued earlier keeps working while it runs'
SELECT s.name,
       CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoSecret') = 'string' THEN cv.client_settings -> 'mtprotoSecret' #>> '{}' ELSE '' END <> '' AND COALESCE(CASE WHEN jsonb_typeof(cv.client_settings -> 'mtprotoPort') = 'number' THEN trunc((cv.client_settings -> 'mtprotoPort')::numeric) END > 0, FALSE) AS snapshot_runs_mtproto_proxy,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND cp.active_protocol = 'mtproto') AS active_mtproto_primary,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_account_protocols p ON p.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND p.protocol = 'mtproto' AND p.active_enabled) AS active_mtproto_rows,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_account_protocols p ON p.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status <> 'active' AND p.protocol = 'mtproto' AND p.active_enabled) AS inactive_accounts_with_mtproto
FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY s.name;

ROLLBACK;
