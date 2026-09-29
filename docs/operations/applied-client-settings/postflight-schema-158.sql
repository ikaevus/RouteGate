-- RouteGate PR #499 (applied client settings): checks AFTER updating Manager.
--
-- Run against the Manager database once the new Manager has started (schema
-- 000158 applied and snapshots of existing versions backfilled at start).
-- Read only: one READ ONLY transaction ending with ROLLBACK. It prints no
-- keys, passwords, secrets or tokens: snapshots are inspected only for
-- structure, and MTProto is reported as a boolean.
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

\echo '   Rows present before the update must all be explicit (compare desired/active counts with preflight P6)'
SELECT s.name, p.protocol,
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
       cv.client_settings ->> 'vpnProtocol' AS applied_node_protocol,
       COALESCE(cv.client_settings ->> 'mtprotoSecret', '') <> ''
         AND COALESCE((cv.client_settings ->> 'mtprotoPort')::int, 0) > 0 AS snapshot_runs_mtproto_proxy
FROM servers s JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY has_snapshot, s.name;

\echo '== Q3. Versions still without a snapshot after the start-up backfill'
SELECT s.name, cv.version, cv.status, COALESCE(s.active_config_version_id = cv.id, FALSE) AS is_active
FROM config_versions cv LEFT JOIN servers s ON s.id = cv.server_id
WHERE cv.client_settings IS NULL
ORDER BY is_active DESC, s.name, cv.version;

-- Mirrors how GET /client-connection, /sub/ and the JSON subscription decide
-- what an active account is served (vpnaccounts.GetActiveClientProtocol,
-- GetOrCreateClientProfile and SubscriptionServer.requireDeployed): the
-- applied primary, or the active protocol rows, each of which must be deployed
-- by the node's active version. It covers the deployment rule only; a link can
-- still be refused for an incomplete setting (for example no Reality key).
\echo '== Q4. What active accounts are served now, per node'
WITH accounts AS (
  SELECT a.id, s.id AS server_id, s.name, s.active_config_version_id IS NULL AS awaiting_first_apply,
         cv.client_settings AS cs,
         cv.client_settings -> 'accounts' -> (a.id::text) AS entry,
         jsonb_typeof(cv.client_settings -> 'accounts') = 'object' AS accounts_known,
         COALESCE(cv.client_settings ->> 'mtprotoSecret', '') <> ''
           AND COALESCE((cv.client_settings ->> 'mtprotoPort')::int, 0) > 0 AS proxy,
         COALESCE(cp.active_protocol,
                  NULLIF(cv.client_settings -> 'accounts' -> (a.id::text) ->> 'primary', ''),
                  cv.client_settings -> 'accounts' -> (a.id::text) -> 'protocols' ->> 0,
                  NULLIF(cv.client_settings ->> 'vpnProtocol', ''),
                  s.vpn_protocol, 'vless') AS raw_primary
  FROM vpn_accounts a
  JOIN servers s ON s.id = a.server_id
  LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
  LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
  WHERE a.status = 'active'
), served AS (
  SELECT ac.*,
         CASE WHEN ac.raw_primary IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
              THEN ac.raw_primary ELSE 'vless' END AS primary_protocol,
         COALESCE((SELECT array_agg(p.protocol ORDER BY p.protocol) FROM vpn_account_protocols p
                   WHERE p.vpn_account_id = ac.id AND p.active_enabled), '{}') AS active_rows
  FROM accounts ac
), evaluated AS (
  SELECT sv.*,
         (SELECT array_agg(proto ORDER BY proto)
          FROM unnest(CASE WHEN cardinality(sv.active_rows) > 0 THEN sv.active_rows
                           ELSE ARRAY[sv.primary_protocol] END) proto
          WHERE NOT (CASE WHEN proto = 'mtproto' THEN sv.proxy
                          WHEN NOT sv.accounts_known THEN TRUE
                          ELSE COALESCE((sv.entry -> 'protocols') ? proto, FALSE) END)) AS undeployed
  FROM served sv
)
SELECT name,
       CASE WHEN awaiting_first_apply THEN 'awaiting_first_apply'
            WHEN cs IS NULL THEN 'served_unchecked_no_snapshot'
            WHEN undeployed IS NULL THEN 'ready'
            ELSE 'awaiting_apply' END AS state,
       count(*) AS active_accounts
FROM evaluated
GROUP BY 1, 2
ORDER BY 1, 2;

\echo '== Q5. Active accounts not served a link now (ids only; compare with preflight P3)'
WITH accounts AS (
  SELECT a.id, s.name, s.active_config_version_id IS NULL AS awaiting_first_apply,
         cv.client_settings AS cs,
         cv.client_settings -> 'accounts' -> (a.id::text) AS entry,
         jsonb_typeof(cv.client_settings -> 'accounts') = 'object' AS accounts_known,
         COALESCE(cv.client_settings ->> 'mtprotoSecret', '') <> ''
           AND COALESCE((cv.client_settings ->> 'mtprotoPort')::int, 0) > 0 AS proxy,
         COALESCE(cp.active_protocol,
                  NULLIF(cv.client_settings -> 'accounts' -> (a.id::text) ->> 'primary', ''),
                  cv.client_settings -> 'accounts' -> (a.id::text) -> 'protocols' ->> 0,
                  NULLIF(cv.client_settings ->> 'vpnProtocol', ''),
                  s.vpn_protocol, 'vless') AS raw_primary
  FROM vpn_accounts a
  JOIN servers s ON s.id = a.server_id
  LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
  LEFT JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
  WHERE a.status = 'active'
), evaluated AS (
  SELECT ac.*, sv.primary_protocol, sv.active_rows,
         (SELECT array_agg(proto ORDER BY proto)
          FROM unnest(CASE WHEN cardinality(sv.active_rows) > 0 THEN sv.active_rows
                           ELSE ARRAY[sv.primary_protocol] END) proto
          WHERE NOT (CASE WHEN proto = 'mtproto' THEN ac.proxy
                          WHEN NOT ac.accounts_known THEN TRUE
                          ELSE COALESCE((ac.entry -> 'protocols') ? proto, FALSE) END)) AS undeployed
  FROM accounts ac
  CROSS JOIN LATERAL (
    SELECT CASE WHEN ac.raw_primary IN ('vless', 'wireguard', 'hysteria2', 'shadowsocks', 'mtproto')
                THEN ac.raw_primary ELSE 'vless' END AS primary_protocol,
           COALESCE((SELECT array_agg(p.protocol ORDER BY p.protocol) FROM vpn_account_protocols p
                     WHERE p.vpn_account_id = ac.id AND p.active_enabled), '{}') AS active_rows
  ) sv
)
SELECT name, id AS account_id, primary_protocol, active_rows,
       CASE WHEN awaiting_first_apply THEN 'awaiting_first_apply' ELSE 'awaiting_apply' END AS state,
       undeployed AS not_deployed_by_active_version
FROM evaluated
WHERE awaiting_first_apply OR (cs IS NOT NULL AND undeployed IS NOT NULL)
ORDER BY name, account_id;

\echo '== Q6. MTProto per node (boolean only): proxy in the applied snapshot and accounts served through it'
\echo '   The proxy has one node-wide secret: any MTProto link issued earlier keeps working while it runs'
SELECT s.name,
       COALESCE(cv.client_settings ->> 'mtprotoSecret', '') <> ''
         AND COALESCE((cv.client_settings ->> 'mtprotoPort')::int, 0) > 0 AS snapshot_runs_mtproto_proxy,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_client_profiles cp ON cp.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND cp.active_protocol = 'mtproto') AS active_mtproto_primary,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_account_protocols p ON p.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status = 'active' AND p.protocol = 'mtproto' AND p.active_enabled) AS active_mtproto_rows,
       (SELECT count(*) FROM vpn_accounts a JOIN vpn_account_protocols p ON p.vpn_account_id = a.id
         WHERE a.server_id = s.id AND a.status <> 'active' AND p.protocol = 'mtproto' AND p.active_enabled) AS inactive_accounts_with_mtproto
FROM servers s LEFT JOIN config_versions cv ON cv.id = s.active_config_version_id
ORDER BY s.name;

ROLLBACK;
