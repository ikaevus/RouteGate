#!/usr/bin/env bash

# Role-aware RouteGate host update primitives.
#
# This library layers Management/VPN/Hybrid ownership rules over
# routegate-update-core.sh. It performs no network access and exposes no
# administrator-facing API.

set -Eeuo pipefail

if ! declare -F rg_update_path >/dev/null 2>&1; then
  printf '[routegate-update] ERROR: routegate-update-core.sh must be sourced first\n' >&2
  return 1 2>/dev/null || exit 1
fi

RG_UPDATE_ROLE_MARKER=${RG_UPDATE_ROLE_MARKER:-/etc/routegate/node-role}
RG_UPDATE_ROLE=""

rg_update_validate_role() {
  case "$1" in
    management|vpn|hybrid) return 0 ;;
    *)
      rg_update_die "unsupported RouteGate node role: ${1:-missing}"
      return 1
      ;;
  esac
}

rg_update_role_has_management() {
  [[ "$1" == "management" || "$1" == "hybrid" ]]
}

rg_update_role_has_vpn() {
  [[ "$1" == "vpn" || "$1" == "hybrid" ]]
}

rg_update_marker_role() {
  local marker
  local role
  marker=$(rg_update_path "$RG_UPDATE_ROLE_MARKER")

  [[ -e "$marker" || -L "$marker" ]] || return 1
  [[ -f "$marker" && ! -L "$marker" ]] || {
    rg_update_die "node role marker must be a regular file: $marker"
    return 2
  }

  role=$(sed -n '1p' "$marker")
  [[ $(wc -l <"$marker") -le 1 ]] || {
    rg_update_die "node role marker must contain exactly one role"
    return 2
  }
  rg_update_validate_role "$role" || return 2
  printf '%s\n' "$role"
}

rg_update_management_layout_complete() {
  local path
  for path in \
    /usr/local/bin/routegate-manager \
    /opt/routegate-manager/migrations \
    /var/www/routegate/index.html \
    /etc/systemd/system/routegate-manager.service \
    /etc/routegate/manager.env; do
    path=$(rg_update_path "$path")
    [[ -e "$path" && ! -L "$path" ]] || return 1
  done
}

rg_update_vpn_layout_complete() {
  local path
  for path in \
    /usr/local/bin/routegate-agent \
    /etc/systemd/system/routegate-agent.service \
    /etc/routegate/agent.yaml; do
    path=$(rg_update_path "$path")
    [[ -e "$path" && ! -L "$path" ]] || return 1
  done
}

rg_update_has_any_management_layout() {
  local path
  for path in \
    /usr/local/bin/routegate-manager \
    /opt/routegate-manager/migrations \
    /var/www/routegate/index.html \
    /etc/systemd/system/routegate-manager.service \
    /etc/routegate/manager.env; do
    path=$(rg_update_path "$path")
    if [[ -e "$path" || -L "$path" ]]; then
      return 0
    fi
  done
  return 1
}

rg_update_has_any_vpn_layout() {
  local path
  for path in \
    /usr/local/bin/routegate-agent \
    /etc/systemd/system/routegate-agent.service \
    /etc/routegate/agent.yaml; do
    path=$(rg_update_path "$path")
    if [[ -e "$path" || -L "$path" ]]; then
      return 0
    fi
  done
  return 1
}

rg_update_infer_legacy_role() {
  local management_complete=0
  local vpn_complete=0
  local management_any=0
  local vpn_any=0

  rg_update_management_layout_complete && management_complete=1
  rg_update_vpn_layout_complete && vpn_complete=1
  rg_update_has_any_management_layout && management_any=1
  rg_update_has_any_vpn_layout && vpn_any=1

  if [[ "$management_any" == "1" && "$management_complete" != "1" ]]; then
    rg_update_die "cannot infer node role from a partial Management layout"
    return 1
  fi
  if [[ "$vpn_any" == "1" && "$vpn_complete" != "1" ]]; then
    rg_update_die "cannot infer node role from a partial VPN layout"
    return 1
  fi

  case "${management_complete}:${vpn_complete}" in
    1:1) printf 'hybrid\n' ;;
    1:0) printf 'management\n' ;;
    0:1) printf 'vpn\n' ;;
    *)
      rg_update_die "cannot infer RouteGate node role from this host layout"
      return 1
      ;;
  esac
}

rg_update_resolve_role() {
  local requested=${1:-auto}
  local detected=""
  local marker_rc=0

  if [[ "$requested" != "auto" ]]; then
    rg_update_validate_role "$requested"
  fi

  detected=$(rg_update_marker_role) || marker_rc=$?
  if [[ "$marker_rc" == "2" ]]; then
    return 1
  fi
  if [[ "$marker_rc" == "1" ]]; then
    detected=$(rg_update_infer_legacy_role)
  fi

  rg_update_validate_role "$detected"
  if [[ "$requested" != "auto" && "$requested" != "$detected" ]]; then
    rg_update_die "requested node role '$requested' does not match detected role '$detected'"
    return 1
  fi

  RG_UPDATE_ROLE=$detected
  printf '%s\n' "$detected"
}

rg_update_role_preflight() {
  local role=$1
  local service state
  rg_update_validate_role "$role"

  if rg_update_role_has_management "$role"; then
    rg_update_management_layout_complete || {
      rg_update_die "Management node layout is incomplete"
      return 1
    }
    service="$RG_UPDATE_MANAGER_SERVICE"
    state=$(systemctl is-active "$service" 2>/dev/null || true)
    rg_update_log "preflight role=${role} service=${service} state=${state:-unknown}"
    [[ "$state" == "active" ]] || {
      rg_update_die "Management service is not active: $service"
      return 1
    }
  fi

  if rg_update_role_has_vpn "$role"; then
    rg_update_vpn_layout_complete || {
      rg_update_die "VPN node layout is incomplete"
      return 1
    }
    service="$RG_UPDATE_AGENT_SERVICE"
    state=$(systemctl is-active "$service" 2>/dev/null || true)
    rg_update_log "preflight role=${role} service=${service} state=${state:-unknown}"
    [[ "$state" == "active" ]] || {
      rg_update_die "VPN Agent service is not active: $service"
      return 1
    }
  fi
}

rg_update_read_manager_database_url() {
  local env_file
  local line value=""
  env_file=$(rg_update_path /etc/routegate/manager.env)

  [[ -f "$env_file" && ! -L "$env_file" ]] || {
    rg_update_die "Manager environment is unavailable"
    return 1
  }

  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      ROUTEGATE_DATABASE_URL=*)
        [[ -z "$value" ]] || {
          rg_update_die "Manager environment contains duplicate database URL entries"
          return 1
        }
        value=${line#ROUTEGATE_DATABASE_URL=}
        if [[ "$value" == \"*\" && "$value" == *\" ]]; then
          value=${value:1:${#value}-2}
        fi
        ;;
    esac
  done <"$env_file"

  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || {
    rg_update_die "Manager database URL is missing or invalid"
    return 1
  }
  printf '%s\n' "$value"
}

rg_update_write_role_backup_meta() {
  local backup_dir=$1
  local role=$2
  cat >"$backup_dir/role.meta" <<EOF_ROLE_META
FORMAT_VERSION=1
ROLE=$role
CREATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF_ROLE_META
  chmod 0600 "$backup_dir/role.meta"
}

rg_update_create_management_backup() {
  local backup_dir=$1
  local db_url=$2
  local backup_root
  local manager_bin migrations_dir frontend_dir manager_unit manager_env

  rg_update_management_layout_complete || {
    rg_update_die "Management node layout is incomplete"
    return 1
  }
  [[ -n "$db_url" ]] || {
    rg_update_die "database URL is required for Management backup"
    return 1
  }

  backup_root=$(dirname "$backup_dir")
  install -d -m 0700 "$backup_root"
  [[ ! -e "$backup_dir" ]] || {
    rg_update_die "backup directory already exists: $backup_dir"
    return 1
  }
  install -d -m 0700 "$backup_dir"

  manager_bin=$(rg_update_path /usr/local/bin/routegate-manager)
  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations)
  frontend_dir=$(rg_update_path /var/www/routegate)
  manager_unit=$(rg_update_path /etc/systemd/system/routegate-manager.service)
  manager_env=$(rg_update_path /etc/routegate/manager.env)

  cp -a "$manager_bin" "$backup_dir/routegate-manager"
  cp -a "$manager_unit" "$backup_dir/routegate-manager.service"
  cp -a "$manager_env" "$backup_dir/manager.env"
  tar -czf "$backup_dir/manager-migrations.tar.gz" -C "$(dirname "$migrations_dir")" "$(basename "$migrations_dir")"
  tar -czf "$backup_dir/frontend.tar.gz" -C "$(dirname "$frontend_dir")" "$(basename "$frontend_dir")"
  pg_dump --format=custom --no-owner --file="$backup_dir/routegate.pgdump" "$db_url"
  rg_update_write_role_backup_meta "$backup_dir" management
  chmod -R go-rwx "$backup_dir"
  rg_update_log "Management backup complete: $backup_dir"
}

rg_update_create_vpn_backup() {
  local backup_dir=$1
  local backup_root
  local agent_bin agent_unit

  rg_update_vpn_layout_complete || {
    rg_update_die "VPN node layout is incomplete"
    return 1
  }

  backup_root=$(dirname "$backup_dir")
  install -d -m 0700 "$backup_root"
  [[ ! -e "$backup_dir" ]] || {
    rg_update_die "backup directory already exists: $backup_dir"
    return 1
  }
  install -d -m 0700 "$backup_dir"

  agent_bin=$(rg_update_path /usr/local/bin/routegate-agent)
  agent_unit=$(rg_update_path /etc/systemd/system/routegate-agent.service)
  cp -a "$agent_bin" "$backup_dir/routegate-agent"
  cp -a "$agent_unit" "$backup_dir/routegate-agent.service"
  rg_update_write_role_backup_meta "$backup_dir" vpn
  chmod -R go-rwx "$backup_dir"
  rg_update_log "VPN backup complete: $backup_dir"
}

rg_update_create_role_backup() {
  local role=$1
  local backup_dir=$2
  local db_url=${3:-}
  rg_update_validate_role "$role"

  case "$role" in
    management)
      rg_update_create_management_backup "$backup_dir" "$db_url"
      ;;
    vpn)
      rg_update_create_vpn_backup "$backup_dir"
      ;;
    hybrid)
      rg_update_create_backup "$backup_dir" "$db_url"
      rg_update_write_role_backup_meta "$backup_dir" hybrid
      ;;
  esac
}

rg_update_apply_management_files() {
  local work_dir=$1
  local manager_bin migrations_dir frontend_dir manager_unit

  manager_bin=$(rg_update_path /usr/local/bin/routegate-manager)
  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations)
  frontend_dir=$(rg_update_path /var/www/routegate)
  manager_unit=$(rg_update_path /etc/systemd/system/routegate-manager.service)

  systemctl stop "$RG_UPDATE_MANAGER_SERVICE"
  install -m 0755 "$work_dir/bin/routegate-manager" "$manager_bin"
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
  systemctl daemon-reload
  rg_update_log "Management platform files applied; VPN runtimes were left untouched"
}

rg_update_apply_vpn_files() {
  local work_dir=$1
  local agent_bin agent_unit

  agent_bin=$(rg_update_path /usr/local/bin/routegate-agent)
  agent_unit=$(rg_update_path /etc/systemd/system/routegate-agent.service)

  systemctl stop "$RG_UPDATE_AGENT_SERVICE"
  install -m 0755 "$work_dir/bin/routegate-agent" "$agent_bin"
  install -m 0644 "$work_dir/systemd/routegate-agent.service" "$agent_unit"
  systemctl daemon-reload
  rg_update_log "VPN Agent files applied; VPN runtimes were left untouched"
}

rg_update_apply_role_files() {
  local role=$1
  local work_dir=$2
  rg_update_validate_role "$role"

  case "$role" in
    management) rg_update_apply_management_files "$work_dir" ;;
    vpn) rg_update_apply_vpn_files "$work_dir" ;;
    hybrid) rg_update_apply_platform_files "$work_dir" ;;
  esac
}

rg_update_start_and_validate_role() {
  local role=$1
  local db_url=${2:-}
  local expected_schema=${3:-}
  rg_update_validate_role "$role"

  if rg_update_role_has_management "$role"; then
    [[ -n "$db_url" && -n "$expected_schema" ]] || {
      rg_update_die "Management validation requires database URL and expected schema"
      return 1
    }
    rg_update_wait_manager 45
    rg_update_validate_database_schema "$db_url" "$expected_schema"
  fi

  if rg_update_role_has_vpn "$role"; then
    rg_update_wait_agent 30
  fi
}

rg_update_require_role_backup() {
  local backup_dir=$1
  local role=$2
  local meta="$backup_dir/role.meta"
  local stored_role

  [[ -f "$meta" && ! -L "$meta" ]] || {
    rg_update_die "role backup metadata is missing"
    return 1
  }
  stored_role=$(sed -n 's/^ROLE=//p' "$meta" | head -n1)
  [[ "$stored_role" == "$role" ]] || {
    rg_update_die "backup role '$stored_role' does not match transaction role '$role'"
    return 1
  }
}

rg_update_restore_management_backup() {
  local backup_dir=$1
  local db_url=$2
  local restore_database=${3:-0}
  local manager_bin migrations_dir frontend_dir manager_unit manager_env
  local migrations_parent frontend_parent restore_rc=0

  rg_update_require_role_backup "$backup_dir" management
  for required in routegate-manager routegate-manager.service manager.env manager-migrations.tar.gz frontend.tar.gz; do
    [[ -e "$backup_dir/$required" && ! -L "$backup_dir/$required" ]] || {
      rg_update_die "Management backup is incomplete: $required"
      return 1
    }
  done

  manager_bin=$(rg_update_path /usr/local/bin/routegate-manager)
  migrations_dir=$(rg_update_path /opt/routegate-manager/migrations)
  frontend_dir=$(rg_update_path /var/www/routegate)
  manager_unit=$(rg_update_path /etc/systemd/system/routegate-manager.service)
  manager_env=$(rg_update_path /etc/routegate/manager.env)
  migrations_parent=$(dirname "$migrations_dir")
  frontend_parent=$(dirname "$frontend_dir")

  systemctl stop "$RG_UPDATE_MANAGER_SERVICE" >/dev/null 2>&1 || true
  RG_UPDATE_DB_RESTORE_RC=0
  if [[ "$restore_database" == "1" ]]; then
    if [[ -z "$db_url" || ! -s "$backup_dir/routegate.pgdump" ]]; then
      RG_UPDATE_DB_RESTORE_RC=1
    else
      pg_restore --clean --if-exists --no-owner --no-privileges --exit-on-error \
        --dbname="$db_url" "$backup_dir/routegate.pgdump" >/dev/null || restore_rc=$?
      RG_UPDATE_DB_RESTORE_RC=$restore_rc
    fi
  fi

  install -m 0755 "$backup_dir/routegate-manager" "$manager_bin"
  rm -rf "$migrations_dir"
  install -d -m 0755 "$migrations_parent"
  tar -xzf "$backup_dir/manager-migrations.tar.gz" -C "$migrations_parent"
  chown -R "$RG_UPDATE_MANAGER_OWNER" "$migrations_parent"
  rm -rf "$frontend_dir"
  install -d -m 0755 "$frontend_parent"
  tar -xzf "$backup_dir/frontend.tar.gz" -C "$frontend_parent"
  install -m 0644 "$backup_dir/routegate-manager.service" "$manager_unit"
  install -m 0600 "$backup_dir/manager.env" "$manager_env"
  systemctl daemon-reload
  systemctl start "$RG_UPDATE_MANAGER_SERVICE" >/dev/null 2>&1 || true
  rg_update_log "Management rollback attempt completed; backup retained at $backup_dir"
}

rg_update_restore_vpn_backup() {
  local backup_dir=$1
  local agent_bin agent_unit

  rg_update_require_role_backup "$backup_dir" vpn
  for required in routegate-agent routegate-agent.service; do
    [[ -e "$backup_dir/$required" && ! -L "$backup_dir/$required" ]] || {
      rg_update_die "VPN backup is incomplete: $required"
      return 1
    }
  done

  agent_bin=$(rg_update_path /usr/local/bin/routegate-agent)
  agent_unit=$(rg_update_path /etc/systemd/system/routegate-agent.service)
  systemctl stop "$RG_UPDATE_AGENT_SERVICE" >/dev/null 2>&1 || true
  install -m 0755 "$backup_dir/routegate-agent" "$agent_bin"
  install -m 0644 "$backup_dir/routegate-agent.service" "$agent_unit"
  systemctl daemon-reload
  systemctl start "$RG_UPDATE_AGENT_SERVICE" >/dev/null 2>&1 || true
  rg_update_log "VPN rollback attempt completed; VPN runtimes were left untouched; backup retained at $backup_dir"
}

rg_update_restore_role_backup() {
  local role=$1
  local backup_dir=$2
  local db_url=${3:-}
  local restore_database=${4:-0}
  rg_update_validate_role "$role"

  case "$role" in
    management)
      rg_update_restore_management_backup "$backup_dir" "$db_url" "$restore_database"
      ;;
    vpn)
      rg_update_restore_vpn_backup "$backup_dir"
      ;;
    hybrid)
      rg_update_require_role_backup "$backup_dir" hybrid
      rg_update_restore_backup "$backup_dir" "$db_url" "$restore_database"
      ;;
  esac
}
