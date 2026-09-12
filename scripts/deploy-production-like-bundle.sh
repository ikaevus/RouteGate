#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

EXPECTED_COMMIT=${1:?expected commit is required}
BUNDLE_FILE=${2:?bundle path is required}
EXPECTED_BUNDLE_SHA=${3:?bundle sha256 is required}
VALIDATION_SCRIPT=${4:?validation script path is required}
UPDATE_CORE=${5:?update core path is required}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}
NGINX_SITE=${ROUTEGATE_NGINX_SITE:-/etc/nginx/sites-available/routegate}
NGINX_BIN=${ROUTEGATE_NGINX_BIN:-/usr/sbin/nginx}
WORK_DIR=$(mktemp -d /tmp/routegate-production-like.XXXXXX)
BACKUP_DIR=""
BACKUP_SCHEMA=""
NGINX_BACKUP=""
NGINX_MUTATED=0
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

reconcile_subscription_proxy_route() {
  local template="$WORK_DIR/nginx/routegate.conf.example"
  local candidate
  local api_location_count

  [[ -f "$template" && ! -L "$template" ]] \
    || { printf '[production-like] release bundle is missing nginx route template.\n' >&2; return 1; }
  [[ -f "$NGINX_SITE" && ! -L "$NGINX_SITE" ]] \
    || { printf '[production-like] RouteGate nginx site is missing or unsafe: %s\n' "$NGINX_SITE" >&2; return 1; }
  [[ -x "$NGINX_BIN" ]] \
    || { printf '[production-like] nginx binary is unavailable: %s\n' "$NGINX_BIN" >&2; return 1; }

  if grep -Eq '^[[:space:]]*location[[:space:]]+/sub/[[:space:]]*\{' "$NGINX_SITE"; then
    log "nginx subscription proxy route=present"
    return 0
  fi

  grep -Eq '^[[:space:]]*location[[:space:]]+/sub/[[:space:]]*\{' "$template" \
    || { printf '[production-like] bundle nginx template has no /sub/ route.\n' >&2; return 1; }
  grep -Fq 'access_log off;' "$template" \
    || { printf '[production-like] bundle /sub/ route does not disable access logging.\n' >&2; return 1; }

  api_location_count=$(grep -Ec '^[[:space:]]*location[[:space:]]+/api/[[:space:]]*\{' "$NGINX_SITE" || true)
  [[ "$api_location_count" == "1" ]] \
    || { printf '[production-like] expected exactly one /api/ nginx location, found %s.\n' "$api_location_count" >&2; return 1; }

  NGINX_BACKUP="$BACKUP_DIR/nginx-routegate.conf"
  cp -a -- "$NGINX_SITE" "$NGINX_BACKUP" || return 1
  candidate=$(mktemp "$(dirname "$NGINX_SITE")/.routegate-subscription-route.XXXXXX") || return 1

  if ! python3 - "$NGINX_SITE" "$template" "$candidate" <<'PY'
from pathlib import Path
import sys

live_path, template_path, output_path = map(Path, sys.argv[1:])
live = live_path.read_text().splitlines(keepends=True)
template = template_path.read_text().splitlines(keepends=True)


def extract_location(lines, marker):
    starts = [i for i, line in enumerate(lines) if line.strip() == marker]
    if len(starts) != 1:
        raise SystemExit(f"expected one {marker!r} block, found {len(starts)}")
    start = starts[0]
    depth = 0
    block = []
    for line in lines[start:]:
        block.append(line)
        depth += line.count("{") - line.count("}")
        if depth == 0:
            break
    if not block or depth != 0:
        raise SystemExit(f"unterminated {marker!r} block")
    return block


block = extract_location(template, "location /sub/ {")
block_text = "".join(block)
if "access_log off;" not in block_text or "proxy_pass http://127.0.0.1:8080;" not in block_text:
    raise SystemExit("subscription block is missing required security/proxy directives")

api_positions = [i for i, line in enumerate(live) if line.strip() == "location /api/ {"]
if len(api_positions) != 1:
    raise SystemExit(f"expected one live /api/ location, found {len(api_positions)}")
insert_at = api_positions[0]
indent = live[insert_at][: len(live[insert_at]) - len(live[insert_at].lstrip())]
base_indent = block[0][: len(block[0]) - len(block[0].lstrip())]
rendered = []
for line in block:
    if line.strip():
        if not line.startswith(base_indent):
            raise SystemExit("subscription block indentation is inconsistent")
        rendered.append(indent + line[len(base_indent):])
    else:
        rendered.append(line)

if rendered and not rendered[-1].endswith("\n"):
    rendered[-1] += "\n"
rendered.append("\n")
output_path.write_text("".join(live[:insert_at] + rendered + live[insert_at:]))
PY
  then
    rm -f -- "$candidate"
    return 1
  fi

  cat -- "$candidate" > "$NGINX_SITE" || { rm -f -- "$candidate"; return 1; }
  rm -f -- "$candidate"

  if ! "$NGINX_BIN" -t; then
    cp -a -- "$NGINX_BACKUP" "$NGINX_SITE"
    "$NGINX_BIN" -t >/dev/null 2>&1 || true
    printf '[production-like] nginx validation rejected the /sub/ route; original site restored.\n' >&2
    return 1
  fi
  if ! systemctl reload nginx; then
    cp -a -- "$NGINX_BACKUP" "$NGINX_SITE"
    "$NGINX_BIN" -t >/dev/null 2>&1 || true
    systemctl reload nginx >/dev/null 2>&1 || true
    printf '[production-like] nginx reload failed; original site restored.\n' >&2
    return 1
  fi

  NGINX_MUTATED=1
  log "nginx subscription proxy route=reconciled"
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

wait_for_active_agent_jobs() {
  local db_url=$1
  local timeout_seconds=${2:-360}
  local poll_seconds=${3:-2}
  local deadline=$((SECONDS + timeout_seconds))
  local active_jobs
  local quiet_polls=0

  while :; do
    active_jobs=$(psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "
SELECT
  (SELECT count(*) FROM config_apply_jobs WHERE status IN ('pending', 'in_progress')) +
  (SELECT count(*) FROM agent_operation_jobs WHERE status IN ('pending', 'in_progress'));
") || return 1

    if [[ ! "$active_jobs" =~ ^[0-9]+$ ]]; then
      printf '[production-like] invalid active Agent job count: %s\n' "$active_jobs" >&2
      return 1
    fi

    if ((active_jobs == 0)); then
      quiet_polls=$((quiet_polls + 1))
      if ((quiet_polls >= 2)); then
        log "active Agent job drain complete"
        return 0
      fi
    else
      quiet_polls=0
      log "waiting for ${active_jobs} active Agent job(s) before platform deploy"
    fi

    if ((SECONDS >= deadline)); then
      printf '[production-like] timed out waiting for active Agent jobs to finish.\n' >&2
      return 1
    fi
    sleep "$poll_seconds"
  done
}

capture_manager_failure_diagnostics() {
  local active_state sub_state result restarts exec_code exec_status
  local journal_file failure_class migration_version

  active_state=$(systemctl show --property=ActiveState --value "$RG_UPDATE_MANAGER_SERVICE" 2>/dev/null || true)
  sub_state=$(systemctl show --property=SubState --value "$RG_UPDATE_MANAGER_SERVICE" 2>/dev/null || true)
  result=$(systemctl show --property=Result --value "$RG_UPDATE_MANAGER_SERVICE" 2>/dev/null || true)
  restarts=$(systemctl show --property=NRestarts --value "$RG_UPDATE_MANAGER_SERVICE" 2>/dev/null || true)
  exec_code=$(systemctl show --property=ExecMainCode --value "$RG_UPDATE_MANAGER_SERVICE" 2>/dev/null || true)
  exec_status=$(systemctl show --property=ExecMainStatus --value "$RG_UPDATE_MANAGER_SERVICE" 2>/dev/null || true)
  log "manager failure active=${active_state:-unknown} sub=${sub_state:-unknown} result=${result:-unknown} restarts=${restarts:-unknown} exec-code=${exec_code:-unknown} exec-status=${exec_status:-unknown}"

  [[ -n "$BACKUP_DIR" && -d "$BACKUP_DIR" ]] || return 0
  command -v journalctl >/dev/null 2>&1 || {
    log "manager failure journal=unavailable"
    return 0
  }

  journal_file="$BACKUP_DIR/manager-failure.log"
  journalctl -u "$RG_UPDATE_MANAGER_SERVICE" -n 120 --no-pager -o cat >"$journal_file" 2>/dev/null || true
  chmod 0600 "$journal_file" 2>/dev/null || true

  failure_class=startup
  migration_version=""
  if grep -Eqi 'apply migration|record migration|commit migration' "$journal_file"; then
    failure_class=migration
    migration_version=$(grep -Eio 'migration[[:space:]]+[A-Za-z0-9._-]+' "$journal_file" | tail -n 1 | awk '{print $2}' || true)
  elif grep -Eqi 'SQLSTATE|postgres|database|pgx' "$journal_file"; then
    failure_class=database
  elif grep -Eqi 'address already in use|bind:' "$journal_file"; then
    failure_class=listener-conflict
  elif grep -Eqi 'permission denied|operation not permitted' "$journal_file"; then
    failure_class=permission
  fi

  if [[ -n "$migration_version" ]]; then
    log "manager failure class=${failure_class} migration=${migration_version} raw-journal=retained-on-host"
  else
    log "manager failure class=${failure_class} raw-journal=retained-on-host"
  fi
}

rollback_database_to_backup() {
  local backup_dir=$1
  local db_url=$2
  local target_schema current_schema migrations_dir version down_file
  local target_seen=0
  local restore_rc=0

  RG_UPDATE_DB_RESTORE_RC=0

  [[ -n "$db_url" && -s "$backup_dir/routegate.pgdump" ]] || {
    RG_UPDATE_DB_RESTORE_RC=1
    printf '[production-like] WARNING: database restore requested but database backup is unavailable\n' >&2
    return 1
  }

  target_schema=$(sed -n 's/^DATABASE_SCHEMA=//p' "$backup_dir/backup.meta" | head -n 1)
  [[ "$target_schema" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || {
    RG_UPDATE_DB_RESTORE_RC=1
    printf '[production-like] WARNING: backup database schema metadata is missing or invalid\n' >&2
    return 1
  }

  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations) || {
    RG_UPDATE_DB_RESTORE_RC=1
    return 1
  }

  current_schema=$(psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1") || {
    RG_UPDATE_DB_RESTORE_RC=1
    return 1
  }

  if [[ "$current_schema" != "$target_schema" ]]; then
    log "database rollback schema current=${current_schema:-missing} target=${target_schema}"
    while IFS= read -r version; do
      [[ -n "$version" ]] || continue
      if [[ "$version" == "$target_schema" ]]; then
        target_seen=1
        break
      fi
      [[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || {
        RG_UPDATE_DB_RESTORE_RC=1
        printf '[production-like] WARNING: unsafe migration identifier during rollback\n' >&2
        return 1
      }
      down_file="$migrations_dir/${version}.down.sql"
      [[ -f "$down_file" && ! -L "$down_file" ]] || {
        RG_UPDATE_DB_RESTORE_RC=1
        printf '[production-like] WARNING: missing safe down migration for %s; database rollback stopped\n' "$version" >&2
        return 1
      }

      log "database rollback migration=${version}"
      if ! psql "$db_url" -v ON_ERROR_STOP=1 -f "$down_file" >/dev/null; then
        RG_UPDATE_DB_RESTORE_RC=1
        printf '[production-like] WARNING: down migration failed for %s; database rollback stopped\n' "$version" >&2
        return 1
      fi
      if ! psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "DELETE FROM schema_migrations WHERE version = '$version';" >/dev/null; then
        RG_UPDATE_DB_RESTORE_RC=1
        printf '[production-like] WARNING: failed to record rollback of %s\n' "$version" >&2
        return 1
      fi
    done < <(psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC")

    if ((target_seen == 0)); then
      RG_UPDATE_DB_RESTORE_RC=1
      printf '[production-like] WARNING: backup schema %s is not present in current migration history\n' "$target_schema" >&2
      return 1
    fi

    current_schema=$(psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1") || {
      RG_UPDATE_DB_RESTORE_RC=1
      return 1
    }
    [[ "$current_schema" == "$target_schema" ]] || {
      RG_UPDATE_DB_RESTORE_RC=1
      printf '[production-like] WARNING: database down-migration stopped at %s instead of %s\n' "${current_schema:-missing}" "$target_schema" >&2
      return 1
    }
  fi

  pg_restore \
    --clean \
    --if-exists \
    --no-owner \
    --no-privileges \
    --exit-on-error \
    --dbname="$db_url" \
    "$backup_dir/routegate.pgdump" >/dev/null || restore_rc=$?

  RG_UPDATE_DB_RESTORE_RC=$restore_rc
  if ((restore_rc != 0)); then
    printf '[production-like] WARNING: database restore failed after schema rollback (exit %d)\n' "$restore_rc" >&2
    return "$restore_rc"
  fi

  log "database rollback restored backup schema=${target_schema}"
  return 0
}

cleanup() {
  rm -rf "$WORK_DIR"
  rm -f "$BUNDLE_FILE" "$VALIDATION_SCRIPT" "$UPDATE_CORE"
}

rollback() {
  local rc=$?
  local rollback_rc=0
  local db_rollback_rc=0
  trap - ERR
  set +e

  if [[ "$MUTATED" == "1" && -n "$BACKUP_DIR" ]]; then
    log "Failure at stage=${STAGE}; restoring production-like baseline."

    if [[ "$DB_MAY_BE_MUTATED" == "1" ]]; then
      rollback_database_to_backup "$BACKUP_DIR" "$DB_URL" || db_rollback_rc=$?
    else
      RG_UPDATE_DB_RESTORE_RC=0
    fi

    rg_update_restore_backup "$BACKUP_DIR" "$DB_URL" 0 || rollback_rc=$?
    set +e
    if [[ "$NGINX_MUTATED" == "1" && -n "$NGINX_BACKUP" && -f "$NGINX_BACKUP" ]]; then
      cp -a -- "$NGINX_BACKUP" "$NGINX_SITE" || rollback_rc=1
      "$NGINX_BIN" -t >/dev/null 2>&1 || rollback_rc=1
      systemctl reload nginx >/dev/null 2>&1 || rollback_rc=1
    fi
    if ((db_rollback_rc != 0)); then
      rollback_rc=1
    fi
    if ((rollback_rc != 0)); then
      printf '[production-like] WARNING: rollback reported an incomplete restore. Backup retained at %s\n' "$BACKUP_DIR" >&2
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
rg_update_require_commands curl date find grep pg_dump pg_restore psql python3 sha256sum tar systemctl
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
BACKUP_SCHEMA=$(psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1")
[[ "$BACKUP_SCHEMA" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] \
  || { printf '[production-like] invalid current database schema before backup: %s\n' "${BACKUP_SCHEMA:-missing}" >&2; exit 1; }
BACKUP_DIR="/root/routegate-backups/rg96-${EXPECTED_COMMIT}-$(date -u +%Y%m%dT%H%M%SZ)"
rg_update_create_backup "$BACKUP_DIR" "$DB_URL"
printf 'DATABASE_SCHEMA=%s\n' "$BACKUP_SCHEMA" >>"$BACKUP_DIR/backup.meta"
chmod 0600 "$BACKUP_DIR/backup.meta"
log "backup database schema=${BACKUP_SCHEMA}"

# The production-like deploy replaces and restarts Manager and Agent binaries.
# Do not interrupt a protocol activation or runtime operation that is already
# being processed. Two consecutive idle polls also narrow the race with an
# operation that was queued while the backup was being created.
STAGE=drain_agent_jobs
wait_for_active_agent_jobs "$DB_URL" 360 2

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
if ! rg_update_wait_manager 45; then
  capture_manager_failure_diagnostics
  false
fi

STAGE=schema_validation
rg_update_validate_database_schema "$DB_URL" "$EXPECTED_SCHEMA"

STAGE=agent_start
rg_update_wait_agent 30

STAGE=nginx_subscription_proxy
reconcile_subscription_proxy_route

STAGE=observability_validation
chmod 0700 "$VALIDATION_SCRIPT"
"$VALIDATION_SCRIPT" "$EXPECTED_COMMIT"

STAGE=final_health
systemctl is-active --quiet routegate-manager
systemctl is-active --quiet routegate-agent
public_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
[[ "$public_status" == 200 ]]
subscription_probe_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/sub/routegate-deploy-probe")
[[ "$subscription_probe_status" == 404 ]]
log "subscription proxy probe=http_${subscription_probe_status}"
log_runtime_diagnostics

STAGE=complete
trap - ERR
log "production-like deploy and validation PASSED"
log "deployed_commit=$EXPECTED_COMMIT"
log "backup=$BACKUP_DIR"