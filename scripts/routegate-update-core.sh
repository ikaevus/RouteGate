#!/usr/bin/env bash

# Shared RouteGate host-update primitives.
#
# This file is a library, not a standalone updater. Callers must provide the
# transaction boundary (locking, policy, provenance verification, and rollback
# trap) and must run privileged operations as root.

set -Eeuo pipefail

RG_UPDATE_LOG_PREFIX=${RG_UPDATE_LOG_PREFIX:-[routegate-update]}
RG_UPDATE_ROOT=${RG_UPDATE_ROOT:-}
RG_UPDATE_MANAGER_SERVICE=${RG_UPDATE_MANAGER_SERVICE:-routegate-manager}
RG_UPDATE_AGENT_SERVICE=${RG_UPDATE_AGENT_SERVICE:-routegate-agent}
RG_UPDATE_HEALTH_URL=${RG_UPDATE_HEALTH_URL:-http://127.0.0.1:8080/api/admin/health}
RG_UPDATE_MANAGER_OWNER=${RG_UPDATE_MANAGER_OWNER:-routegate:routegate}

RG_UPDATE_BUNDLE_VERSION=""
RG_UPDATE_BUNDLE_COMMIT=""
RG_UPDATE_EXPECTED_SCHEMA=""
RG_UPDATE_DB_RESTORE_RC=0

rg_update_log() {
  printf '%s %s\n' "$RG_UPDATE_LOG_PREFIX" "$*"
}

rg_update_die() {
  printf '%s ERROR: %s\n' "$RG_UPDATE_LOG_PREFIX" "$*" >&2
  return 1
}

rg_update_path() {
  local absolute=$1
  [[ "$absolute" == /* ]] || {
    rg_update_die "internal path must be absolute: $absolute"
    return 1
  }
  if [[ -n "$RG_UPDATE_ROOT" ]]; then
    printf '%s%s' "${RG_UPDATE_ROOT%/}" "$absolute"
  else
    printf '%s' "$absolute"
  fi
}

rg_update_require_root() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || rg_update_die "host update operations must run as root"
}

rg_update_require_commands() {
  local command_name
  for command_name in "$@"; do
    command -v "$command_name" >/dev/null 2>&1 || {
      rg_update_die "missing required command: $command_name"
      return 1
    }
  done
}

rg_update_validate_archive() {
  local bundle=$1

  [[ -f "$bundle" && ! -L "$bundle" ]] || {
    rg_update_die "release bundle must be a regular file: $bundle"
    return 1
  }

  if tar -tzf "$bundle" | awk '$0 ~ /^\// || $0 ~ /(^|\/)\.\.(\/|$)/ {found=1} END {exit !found}'; then
    rg_update_die "release bundle contains an unsafe path"
    return 1
  fi

  if tar -tvzf "$bundle" | awk '$1 ~ /^[lhbcp]/ {found=1} END {exit !found}'; then
    rg_update_die "release bundle contains a link or special filesystem entry"
    return 1
  fi
}

rg_update_read_bundle_metadata() {
  local manifest=$1
  local expected_commit=$2
  local expected_os=$3
  local expected_arch=$4
  local key value
  local format_version=""
  local version=""
  local commit=""
  local build_date=""
  local os_name=""
  local arch=""

  [[ -f "$manifest" && ! -L "$manifest" ]] || {
    rg_update_die "release bundle is missing metadata/manifest.env"
    return 1
  }

  while IFS='=' read -r key value || [[ -n "$key" ]]; do
    [[ -n "$key" ]] || continue
    case "$key" in
      FORMAT_VERSION) format_version=$value ;;
      VERSION) version=$value ;;
      COMMIT) commit=$value ;;
      BUILD_DATE) build_date=$value ;;
      OS) os_name=$value ;;
      ARCH) arch=$value ;;
      *)
        rg_update_die "unsupported bundle metadata key: $key"
        return 1
        ;;
    esac
  done <"$manifest"

  [[ "$format_version" == "1" ]] || {
    rg_update_die "unsupported bundle metadata format: ${format_version:-missing}"
    return 1
  }
  [[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || {
    rg_update_die "invalid bundle version metadata"
    return 1
  }
  [[ "$commit" =~ ^[a-f0-9]{40}$ ]] || {
    rg_update_die "invalid bundle commit metadata"
    return 1
  }
  [[ "$build_date" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || {
    rg_update_die "invalid bundle build-date metadata"
    return 1
  }
  [[ "$commit" == "$expected_commit" ]] || {
    rg_update_die "bundle commit does not match the requested commit"
    return 1
  }
  [[ "$os_name" == "$expected_os" ]] || {
    rg_update_die "bundle OS does not match the target host"
    return 1
  }
  [[ "$arch" == "$expected_arch" ]] || {
    rg_update_die "bundle architecture does not match the target host"
    return 1
  }

  RG_UPDATE_BUNDLE_VERSION=$version
  RG_UPDATE_BUNDLE_COMMIT=$commit
}

rg_update_expected_schema_from_dir() {
  local migrations_dir=$1
  local expected

  expected=$(
    find "$migrations_dir" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' \
      | LC_ALL=C sort \
      | tail -n 1 \
      | sed 's/\.up\.sql$//'
  )
  [[ -n "$expected" ]] || {
    rg_update_die "release bundle contains no database migrations"
    return 1
  }
  [[ "$expected" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || {
    rg_update_die "release bundle contains an invalid migration identifier"
    return 1
  }

  RG_UPDATE_EXPECTED_SCHEMA=$expected
  printf '%s\n' "$expected"
}

rg_update_verify_and_extract_bundle() {
  local bundle=$1
  local expected_sha=$2
  local expected_commit=$3
  local expected_os=$4
  local expected_arch=$5
  local work_dir=$6
  local actual_sha

  [[ "$expected_sha" =~ ^[a-f0-9]{64}$ ]] || {
    rg_update_die "expected bundle SHA-256 is invalid"
    return 1
  }
  [[ "$expected_commit" =~ ^[a-f0-9]{40}$ ]] || {
    rg_update_die "expected commit is invalid"
    return 1
  }
  [[ "$expected_os" =~ ^[a-z0-9][a-z0-9._-]*$ ]] || {
    rg_update_die "expected OS is invalid"
    return 1
  }
  [[ "$expected_arch" =~ ^[a-z0-9][a-z0-9._-]*$ ]] || {
    rg_update_die "expected architecture is invalid"
    return 1
  }

  actual_sha=$(sha256sum "$bundle" | awk '{print $1}')
  [[ "$actual_sha" == "$expected_sha" ]] || {
    rg_update_die "bundle SHA-256 mismatch"
    return 1
  }

  rg_update_validate_archive "$bundle"
  install -d -m 0700 "$work_dir"
  tar -xzf "$bundle" -C "$work_dir" --no-same-owner --no-same-permissions

  for required in \
    bin/routegate-manager \
    bin/routegate-agent \
    frontend/index.html \
    manager/migrations \
    systemd/routegate-manager.service \
    systemd/routegate-agent.service \
    metadata/manifest.env; do
    [[ -e "$work_dir/$required" && ! -L "$work_dir/$required" ]] || {
      rg_update_die "release bundle is missing required entry: $required"
      return 1
    }
  done

  rg_update_read_bundle_metadata \
    "$work_dir/metadata/manifest.env" \
    "$expected_commit" \
    "$expected_os" \
    "$expected_arch"
  rg_update_expected_schema_from_dir "$work_dir/manager/migrations" >/dev/null
  rg_update_log "verified bundle version=${RG_UPDATE_BUNDLE_VERSION} commit=${RG_UPDATE_BUNDLE_COMMIT} schema=${RG_UPDATE_EXPECTED_SCHEMA}"
}

rg_update_require_hybrid_layout() {
  local path
  for path in \
    /usr/local/bin/routegate-manager \
    /usr/local/bin/routegate-agent \
    /opt/routegate-manager/migrations \
    /var/www/routegate \
    /etc/systemd/system/routegate-manager.service \
    /etc/systemd/system/routegate-agent.service \
    /etc/routegate/manager.env; do
    path=$(rg_update_path "$path")
    [[ -e "$path" && ! -L "$path" ]] || {
      rg_update_die "required RouteGate platform path is missing or unsafe: $path"
      return 1
    }
  done
}

rg_update_control_plane_preflight() {
  local service state
  for service in "$RG_UPDATE_MANAGER_SERVICE" "$RG_UPDATE_AGENT_SERVICE"; do
    state=$(systemctl is-active "$service" 2>/dev/null || true)
    rg_update_log "preflight control-plane ${service}=${state:-unknown}"
    [[ "$state" == "active" ]] || {
      rg_update_die "control-plane service is not active: $service"
      return 1
    }
  done
}

rg_update_create_backup() {
  local backup_dir=$1
  local db_url=$2
  local backup_root
  local manager_bin agent_bin migrations_dir frontend_dir manager_unit agent_unit manager_env

  [[ -n "$db_url" ]] || {
    rg_update_die "database URL is required for backup"
    return 1
  }

  rg_update_require_hybrid_layout

  backup_root=$(dirname "$backup_dir")
  install -d -m 0700 "$backup_root"
  [[ ! -e "$backup_dir" ]] || {
    rg_update_die "backup directory already exists: $backup_dir"
    return 1
  }
  install -d -m 0700 "$backup_dir"

  manager_bin=$(rg_update_path /usr/local/bin/routegate-manager)
  agent_bin=$(rg_update_path /usr/local/bin/routegate-agent)
  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations)
  frontend_dir=$(rg_update_path /var/www/routegate)
  manager_unit=$(rg_update_path /etc/systemd/system/routegate-manager.service)
  agent_unit=$(rg_update_path /etc/systemd/system/routegate-agent.service)
  manager_env=$(rg_update_path /etc/routegate/manager.env)

  cp -a "$manager_bin" "$backup_dir/routegate-manager"
  cp -a "$agent_bin" "$backup_dir/routegate-agent"
  cp -a "$manager_unit" "$backup_dir/routegate-manager.service"
  cp -a "$agent_unit" "$backup_dir/routegate-agent.service"
  cp -a "$manager_env" "$backup_dir/manager.env"
  tar -czf "$backup_dir/manager-migrations.tar.gz" -C "$(dirname "$migrations_dir")" "$(basename "$migrations_dir")"
  tar -czf "$backup_dir/frontend.tar.gz" -C "$(dirname "$frontend_dir")" "$(basename "$frontend_dir")"
  pg_dump --format=custom --no-owner --file="$backup_dir/routegate.pgdump" "$db_url"

  cat >"$backup_dir/backup.meta" <<EOF_BACKUP_META
FORMAT_VERSION=1
CREATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF_BACKUP_META

  chmod -R go-rwx "$backup_dir"
  rg_update_log "backup complete: $backup_dir"
}

rg_update_apply_platform_files() {
  local work_dir=$1
  local manager_bin agent_bin migrations_dir frontend_dir manager_unit agent_unit

  manager_bin=$(rg_update_path /usr/local/bin/routegate-manager)
  agent_bin=$(rg_update_path /usr/local/bin/routegate-agent)
  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations)
  frontend_dir=$(rg_update_path /var/www/routegate)
  manager_unit=$(rg_update_path /etc/systemd/system/routegate-manager.service)
  agent_unit=$(rg_update_path /etc/systemd/system/routegate-agent.service)

  systemctl stop "$RG_UPDATE_AGENT_SERVICE"
  systemctl stop "$RG_UPDATE_MANAGER_SERVICE"

  install -m 0755 "$work_dir/bin/routegate-manager" "$manager_bin"
  install -m 0755 "$work_dir/bin/routegate-agent" "$agent_bin"

  rm -rf "$migrations_dir"
  cp -a "$work_dir/manager/migrations" "$migrations_dir"
  chown -R "$RG_UPDATE_MANAGER_OWNER" "$(dirname "$migrations_dir")"

  install -d -m 0755 "$frontend_dir"
  find "$frontend_dir" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
  cp -a "$work_dir/frontend/." "$frontend_dir/"
  chown -R root:root "$frontend_dir"
  find "$frontend_dir" -type d -exec chmod 0755 {} +
  find "$frontend_dir" -type f -exec chmod 0644 {} +

  install -m 0644 "$work_dir/systemd/routegate-manager.service" "$manager_unit"
  install -m 0644 "$work_dir/systemd/routegate-agent.service" "$agent_unit"
  systemctl daemon-reload

  rg_update_log "platform files applied; VPN runtimes were left untouched"
}

rg_update_wait_manager() {
  local attempts=${1:-45}
  local i

  systemctl start "$RG_UPDATE_MANAGER_SERVICE"
  for ((i = 0; i < attempts; i++)); do
    if curl -fsS "$RG_UPDATE_HEALTH_URL" >/dev/null 2>&1; then
      rg_update_log "manager health check passed"
      return 0
    fi
    sleep 1
  done

  rg_update_die "manager did not become healthy within ${attempts}s"
}

rg_update_validate_database_schema() {
  local db_url=$1
  local expected_schema=$2
  local applied_schema

  applied_schema=$(psql "$db_url" -qAtc "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
  [[ "$applied_schema" == "$expected_schema" ]] || {
    rg_update_die "database schema mismatch after update: applied=${applied_schema:-missing} expected=$expected_schema"
    return 1
  }
  rg_update_log "database schema=${applied_schema}"
}

rg_update_wait_agent() {
  local attempts=${1:-30}
  local i

  systemctl start "$RG_UPDATE_AGENT_SERVICE"
  for ((i = 0; i < attempts; i++)); do
    if systemctl is-active --quiet "$RG_UPDATE_AGENT_SERVICE"; then
      rg_update_log "agent service is active"
      return 0
    fi
    sleep 1
  done

  rg_update_die "agent did not become active within ${attempts}s"
}

rg_update_restore_backup() {
  local backup_dir=$1
  local db_url=$2
  local restore_database=${3:-0}
  local manager_bin agent_bin migrations_dir frontend_dir manager_unit agent_unit manager_env
  local migrations_parent frontend_parent
  local restore_rc=0

  [[ -d "$backup_dir" && ! -L "$backup_dir" ]] || {
    rg_update_die "backup directory is missing or unsafe: $backup_dir"
    return 1
  }

  for required in \
    routegate-manager \
    routegate-agent \
    routegate-manager.service \
    routegate-agent.service \
    manager.env \
    manager-migrations.tar.gz \
    frontend.tar.gz; do
    [[ -e "$backup_dir/$required" && ! -L "$backup_dir/$required" ]] || {
      rg_update_die "backup is incomplete: $required"
      return 1
    }
  done

  manager_bin=$(rg_update_path /usr/local/bin/routegate-manager)
  agent_bin=$(rg_update_path /usr/local/bin/routegate-agent)
  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations)
  frontend_dir=$(rg_update_path /var/www/routegate)
  manager_unit=$(rg_update_path /etc/systemd/system/routegate-manager.service)
  agent_unit=$(rg_update_path /etc/systemd/system/routegate-agent.service)
  manager_env=$(rg_update_path /etc/routegate/manager.env)
  migrations_parent=$(dirname "$migrations_dir")
  frontend_parent=$(dirname "$frontend_dir")

  systemctl stop "$RG_UPDATE_AGENT_SERVICE" "$RG_UPDATE_MANAGER_SERVICE" >/dev/null 2>&1 || true

  RG_UPDATE_DB_RESTORE_RC=0
  if [[ "$restore_database" == "1" ]]; then
    if [[ -z "$db_url" || ! -s "$backup_dir/routegate.pgdump" ]]; then
      RG_UPDATE_DB_RESTORE_RC=1
      printf '%s WARNING: database restore requested but database backup is unavailable\n' "$RG_UPDATE_LOG_PREFIX" >&2
    else
      pg_restore \
        --clean \
        --if-exists \
        --no-owner \
        --no-privileges \
        --exit-on-error \
        --dbname="$db_url" \
        "$backup_dir/routegate.pgdump" >/dev/null || restore_rc=$?
      RG_UPDATE_DB_RESTORE_RC=$restore_rc
      if ((RG_UPDATE_DB_RESTORE_RC != 0)); then
        printf '%s WARNING: database restore failed (exit %d); continuing file/service rollback\n' \
          "$RG_UPDATE_LOG_PREFIX" "$RG_UPDATE_DB_RESTORE_RC" >&2
      fi
    fi
  fi

  install -m 0755 "$backup_dir/routegate-manager" "$manager_bin"
  install -m 0755 "$backup_dir/routegate-agent" "$agent_bin"

  rm -rf "$migrations_dir"
  install -d -m 0755 "$migrations_parent"
  tar -xzf "$backup_dir/manager-migrations.tar.gz" -C "$migrations_parent"
  chown -R "$RG_UPDATE_MANAGER_OWNER" "$migrations_parent"

  rm -rf "$frontend_dir"
  install -d -m 0755 "$frontend_parent"
  tar -xzf "$backup_dir/frontend.tar.gz" -C "$frontend_parent"

  install -m 0644 "$backup_dir/routegate-manager.service" "$manager_unit"
  install -m 0644 "$backup_dir/routegate-agent.service" "$agent_unit"
  install -m 0600 "$backup_dir/manager.env" "$manager_env"

  systemctl daemon-reload
  systemctl start "$RG_UPDATE_MANAGER_SERVICE" >/dev/null 2>&1 || true
  systemctl start "$RG_UPDATE_AGENT_SERVICE" >/dev/null 2>&1 || true
  rg_update_log "rollback attempt completed; VPN runtimes were left untouched; backup retained at $backup_dir"
}
