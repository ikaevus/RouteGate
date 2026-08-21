#!/usr/bin/env bash
set -Eeuo pipefail

EXPECTED_COMMIT=${1:?expected commit is required}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}

log() {
  printf '[production-like-deploy] %s\n' "$*"
}

for service in routegate-manager routegate-agent sing-box; do
  systemctl is-active --quiet "$service"
  log "${service}=active"
done

/usr/bin/sing-box check -c /etc/sing-box/config.json >/dev/null

manager_status=$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/admin/health)
[[ "$manager_status" == 200 ]]

public_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
[[ "$public_status" == 200 ]]

[[ -r /etc/routegate/manager.env ]] || { printf 'Missing /etc/routegate/manager.env\n' >&2; exit 1; }
set -a
# shellcheck disable=SC1091
source /etc/routegate/manager.env
set +a
DB_URL=${ROUTEGATE_DATABASE_URL:?ROUTEGATE_DATABASE_URL is required}

client_profile_invariants=$(psql "$DB_URL" -qAtc "
  SELECT
    EXISTS (
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = 'public'
        AND table_name = 'vpn_client_profiles'
        AND column_name = 'protocol'
    ),
    EXISTS (
      SELECT 1
      FROM pg_index AS index_row
      JOIN pg_attribute AS attribute_row
        ON attribute_row.attrelid = index_row.indrelid
       AND attribute_row.attnum = index_row.indkey[0]
      WHERE index_row.indrelid = 'vpn_client_profiles'::regclass
        AND index_row.indisunique
        AND index_row.indisvalid
        AND index_row.indpred IS NULL
        AND index_row.indexprs IS NULL
        AND index_row.indnkeyatts = 1
        AND attribute_row.attname = 'vpn_account_id'
    ),
    EXISTS (
      SELECT 1
      FROM pg_constraint
      WHERE conrelid = 'vpn_client_profiles'::regclass
        AND conname = 'vpn_client_profiles_protocol_check'
    ),
    EXISTS (
      SELECT 1
      FROM pg_trigger
      WHERE tgrelid = 'vpn_client_profiles'::regclass
        AND tgname = 'trg_vpn_client_profiles_mark_server_dirty'
        AND NOT tgisinternal
    )
")
[[ "$client_profile_invariants" == "t|t|t|t" ]] || {
  printf 'Client-profile schema invariant mismatch: %s\n' "$client_profile_invariants" >&2
  exit 1
}
log "client-profile schema invariants=ok"

log "deploy-only validation passed"
log "deployed_commit=$EXPECTED_COMMIT"
