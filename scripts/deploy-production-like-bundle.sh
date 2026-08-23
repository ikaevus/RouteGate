#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

EXPECTED_COMMIT=${1:?expected commit is required}
BUNDLE_FILE=${2:?bundle path is required}
EXPECTED_BUNDLE_SHA=${3:?bundle sha256 is required}
VALIDATION_SCRIPT=${4:?validation script path is required}
UPDATE_CORE=${5:?update core path is required}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}
WORK_DIR=$(mktemp -d /tmp/routegate-production-like.XXXXXX)
BACKUP_DIR=""
DB_URL=""
EXPECTED_SCHEMA=""
MUTATED=0
DB_MAY_BE_MUTATED=0
STAGE=initializing

[[ -r "$UPDATE_CORE" ]] || { printf 'Update core is not readable.\n' >&2; exit 1; }
RG_UPDATE_LOG_PREFIX='[production-like]'
# shellcheck source=scripts/routegate-update-core.sh
source "$UPDATE_CORE"

log() {
  rg_update_log "$*"
}

runtime_status() {
  local label=$1
  local service=$2
  local load_state
  local state

  load_state=$(systemctl show --property=LoadState --value "$service" 2>/dev/null || true)
  if [[ "$load_state" != "loaded" ]]; then
    log "runtime ${label}=not-installed-or-unmanaged"
    return 0
  fi

  state=$(systemctl is-active "$service" 2>/dev/null || true)
  [[ -n "$state" ]] || state=unknown
  log "runtime ${label} service=${service} state=${state}"
}

log_runtime_diagnostics() {
  runtime_status sing-box sing-box
  runtime_status wireguard wg-quick@routegate-wg0
  runtime_status hysteria2 hysteria-server
  runtime_status mtproto routegate-mtproto

  if command -v sing-box >/dev/null 2>&1 && [[ -r /etc/sing-box/config.json ]]; then
    if sing-box check -c /etc/sing-box/config.json >/dev/null 2>&1; then
      log "runtime sing-box config=valid"
    else
      log "runtime sing-box config=invalid"
    fi
  fi
}

cleanup() {
  rm -rf "$WORK_DIR"
  rm -f "$BUNDLE_FILE" "$VALIDATION_SCRIPT" "$UPDATE_CORE"
}

rollback() {
  local rc=$?
  local rollback_rc=0
  trap - ERR
  set +e

  if [[ "$MUTATED" == "1" && -n "$BACKUP_DIR" ]]; then
    log "Failure at stage=${STAGE}; restoring production-like baseline."
    rg_update_restore_backup "$BACKUP_DIR" "$DB_URL" "$DB_MAY_BE_MUTATED" || rollback_rc=$?
    set +e
    if ((rollback_rc != 0)); then
      printf '[production-like] WARNING: file/service rollback reported exit %d. Backup retained at %s\n' \
        "$rollback_rc" "$BACKUP_DIR" >&2
    fi
    if ((RG_UPDATE_DB_RESTORE_RC != 0)); then
      printf '[production-like] WARNING: database restore reported exit %d. Backup retained at %s\n' \
        "$RG_UPDATE_DB_RESTORE_RC" "$BACKUP_DIR" >&2
    fi
  fi

  cleanup
  exit "$rc"
}

trap rollback ERR
trap cleanup EXIT

rg_update_require_root
rg_update_require_commands curl date find pg_dump pg_restore psql sha256sum tar systemctl
[[ -r /etc/routegate/manager.env ]] || { printf 'Missing /etc/routegate/manager.env\n' >&2; exit 1; }
[[ -r "$VALIDATION_SCRIPT" ]] || { printf 'Validation script is not readable.\n' >&2; exit 1; }

STAGE=preflight
rg_update_control_plane_preflight
log_runtime_diagnostics

STAGE=bundle_verification
rg_update_verify_and_extract_bundle \
  "$BUNDLE_FILE" \
  "$EXPECTED_BUNDLE_SHA" \
  "$EXPECTED_COMMIT" \
  linux \
  amd64 \
  "$WORK_DIR"
EXPECTED_SCHEMA=$RG_UPDATE_EXPECTED_SCHEMA

set -a
# shellcheck disable=SC1091
source /etc/routegate/manager.env
set +a
DB_URL=${ROUTEGATE_DATABASE_URL:?ROUTEGATE_DATABASE_URL is required}

STAGE=backup
BACKUP_DIR="/root/routegate-backups/rg96-${EXPECTED_COMMIT}-$(date -u +%Y%m%dT%H%M%SZ)"
rg_update_create_backup "$BACKUP_DIR" "$DB_URL"

STAGE=deploy_files
MUTATED=1
rg_update_apply_platform_files "$WORK_DIR"

if grep -q '^ROUTEGATE_PUBLIC_URL=' /etc/routegate/manager.env; then
  sed -i "s#^ROUTEGATE_PUBLIC_URL=.*#ROUTEGATE_PUBLIC_URL=\"${PUBLIC_URL}\"#" /etc/routegate/manager.env
else
  printf 'ROUTEGATE_PUBLIC_URL="%s"\n' "$PUBLIC_URL" >> /etc/routegate/manager.env
fi
chmod 0600 /etc/routegate/manager.env

STAGE=manager_start
DB_MAY_BE_MUTATED=1
rg_update_wait_manager 45

STAGE=schema_validation
rg_update_validate_database_schema "$DB_URL" "$EXPECTED_SCHEMA"

STAGE=agent_start
rg_update_wait_agent 30

STAGE=observability_validation
chmod 0700 "$VALIDATION_SCRIPT"
"$VALIDATION_SCRIPT" "$EXPECTED_COMMIT"

STAGE=final_health
systemctl is-active --quiet routegate-manager
systemctl is-active --quiet routegate-agent
public_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
[[ "$public_status" == 200 ]]
log_runtime_diagnostics

STAGE=complete
trap - ERR
log "production-like deploy and validation PASSED"
log "deployed_commit=$EXPECTED_COMMIT"
log "backup=$BACKUP_DIR"
