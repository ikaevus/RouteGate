#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

OPERATION=${1:-}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}

log() {
  printf '[routegate-ops] %s\n' "$*"
}

die() {
  printf '[routegate-ops] ERROR: %s\n' "$*" >&2
  exit 1
}

require_root() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || die "must run as root"
}

service_state() {
  local label=$1
  local service=$2
  local load_state active_state sub_state result restarts exec_status exec_code

  load_state=$(systemctl show --property=LoadState --value "$service" 2>/dev/null || true)
  if [[ "$load_state" != "loaded" ]]; then
    log "service=${label} installed=false"
    return 0
  fi

  active_state=$(systemctl show --property=ActiveState --value "$service" 2>/dev/null || true)
  sub_state=$(systemctl show --property=SubState --value "$service" 2>/dev/null || true)
  result=$(systemctl show --property=Result --value "$service" 2>/dev/null || true)
  restarts=$(systemctl show --property=NRestarts --value "$service" 2>/dev/null || true)
  exec_status=$(systemctl show --property=ExecMainStatus --value "$service" 2>/dev/null || true)
  exec_code=$(systemctl show --property=ExecMainCode --value "$service" 2>/dev/null || true)
  log "service=${label} installed=true active=${active_state:-unknown} sub=${sub_state:-unknown} result=${result:-unknown} restarts=${restarts:-unknown} exec-code=${exec_code:-unknown} exec-status=${exec_status:-unknown}"
}

control_plane_diagnostics() {
  service_state postgresql postgresql
  service_state manager routegate-manager.service
  service_state agent routegate-agent.service
  service_state nginx nginx
  service_state certbot-timer certbot.timer
}

runtime_diagnostics() {
  service_state sing-box sing-box.service
  service_state wireguard wg-quick@routegate-wg0.service
  service_state hysteria2 hysteria-server.service
  service_state mtproto routegate-mtproto.service

  if command -v sing-box >/dev/null 2>&1 && [[ -r /etc/sing-box/config.json ]]; then
    if sing-box check -c /etc/sing-box/config.json >/dev/null 2>&1; then
      log "runtime=sing-box config=valid"
    else
      log "runtime=sing-box config=invalid"
    fi
  else
    log "runtime=sing-box config=unavailable"
  fi
}

classify_sing_box_failure() {
  local journal lower failure_class conflict_line conflict_port socket_line owner_process
  journal=$(journalctl -u sing-box.service -n 120 --no-pager -o cat 2>/dev/null || true)
  lower=$(printf '%s' "$journal" | tr '[:upper:]' '[:lower:]')
  failure_class=unknown

  if [[ -z "$lower" ]]; then
    failure_class=no-journal-data
  elif [[ "$lower" == *"address already in use"* ]] || [[ "$lower" == *"bind: address already in use"* ]]; then
    failure_class=address-in-use
  elif [[ "$lower" == *"permission denied"* ]]; then
    failure_class=permission-denied
  elif [[ "$lower" == *"operation not permitted"* ]]; then
    failure_class=operation-not-permitted
  elif [[ "$lower" == *"no such file or directory"* ]]; then
    failure_class=missing-file
  elif [[ "$lower" == *"certificate"* ]] && [[ "$lower" == *"error"* || "$lower" == *"failed"* ]]; then
    failure_class=certificate-error
  elif [[ "$lower" == *"unknown field"* ]] || [[ "$lower" == *"unknown option"* ]] || [[ "$lower" == *"invalid argument"* ]]; then
    failure_class=runtime-compatibility
  elif [[ "$lower" == *"network is unreachable"* ]]; then
    failure_class=network-unreachable
  fi

  log "runtime=sing-box failure-class=${failure_class}"

  if [[ "$failure_class" == address-in-use ]]; then
    conflict_line=$(printf '%s\n' "$journal" | grep -Ei 'address already in use|bind: address already in use' | tail -n 1 || true)
    conflict_port=$(printf '%s\n' "$conflict_line" | grep -Eo ':[0-9]{1,5}' | tail -n 1 | tr -d ':' || true)
    if [[ "$conflict_port" =~ ^[0-9]+$ ]] && (( conflict_port >= 1 && conflict_port <= 65535 )); then
      socket_line=$(ss -H -ltnp "sport = :${conflict_port}" 2>/dev/null | head -n 1 || true)
      if [[ -z "$socket_line" ]]; then
        socket_line=$(ss -H -lunp "sport = :${conflict_port}" 2>/dev/null | head -n 1 || true)
      fi
      owner_process=$(printf '%s\n' "$socket_line" | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | head -n 1)
      log "runtime=sing-box conflict-port=${conflict_port} owner-process=${owner_process:-unknown}"
    else
      log "runtime=sing-box conflict-port=unknown owner-process=unknown"
    fi
  fi
}

sing_box_diagnostics() {
  service_state sing-box sing-box.service
  command -v sing-box >/dev/null 2>&1 || die "sing-box binary is unavailable"
  [[ -r /etc/sing-box/config.json ]] || die "sing-box config is unavailable"

  local version config_state directory_state unit_execstart config_count config_names
  version=$(sing-box version 2>/dev/null | head -n 1 || true)
  if sing-box check -c /etc/sing-box/config.json >/dev/null 2>&1; then
    config_state=valid
  else
    config_state=invalid
  fi
  if sing-box check -C /etc/sing-box >/dev/null 2>&1; then
    directory_state=valid
  else
    directory_state=invalid
  fi

  config_count=$(find /etc/sing-box -maxdepth 1 -type f \( -name '*.json' -o -name '*.jsonc' \) -printf '%f\n' 2>/dev/null | wc -l | tr -d ' ')
  config_names=$(find /etc/sing-box -maxdepth 1 -type f \( -name '*.json' -o -name '*.jsonc' \) -printf '%f\n' 2>/dev/null | LC_ALL=C sort | head -n 20 | paste -sd ',' -)
  unit_execstart=$(systemctl show --property=ExecStart --value sing-box.service 2>/dev/null || true)
  unit_execstart=${unit_execstart//$'\n'/ }
  unit_execstart=${unit_execstart:0:240}

  log "runtime=sing-box version=${version:-unknown}"
  log "runtime=sing-box config=${config_state}"
  log "runtime=sing-box directory-config=${directory_state} files=${config_count:-0} names=${config_names:-none}"
  log "runtime=sing-box unit-execstart=${unit_execstart:-unknown}"
  classify_sing_box_failure

  # Raw journal/config/socket output never leaves the host. Only fixed failure
  # classes plus the conflicting port and process name are exposed.
}

http_diagnostics() {
  local local_status public_status
  local_status=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/admin/health || true)
  public_status=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' "$PUBLIC_URL/" || true)
  log "http=manager-local status=${local_status:-000}"
  log "http=public-root status=${public_status:-000}"
}

schema_diagnostics() {
  if [[ ! -r /etc/routegate/manager.env ]]; then
    log "schema=unavailable reason=manager-env-missing"
    return 0
  fi

  set -a
  # shellcheck disable=SC1091
  source /etc/routegate/manager.env
  set +a

  if [[ -z ${ROUTEGATE_DATABASE_URL:-} ]] || ! command -v psql >/dev/null 2>&1; then
    log "schema=unavailable reason=database-client-or-url-missing"
    return 0
  fi

  local latest_schema
  latest_schema=$(psql "$ROUTEGATE_DATABASE_URL" -qAtc "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1" 2>/dev/null || true)
  if [[ -n "$latest_schema" ]]; then
    log "schema=ok latest=${latest_schema}"
  else
    log "schema=unavailable reason=query-failed"
  fi
}

diagnose() {
  control_plane_diagnostics
  runtime_diagnostics
  http_diagnostics
  schema_diagnostics
}

validate_platform() {
  local service
  for service in postgresql routegate-manager.service routegate-agent.service nginx; do
    systemctl is-active --quiet "$service" || die "required control-plane service is not active: ${service}"
  done

  local local_status public_status
  local_status=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/admin/health)
  [[ "$local_status" == 200 ]] || die "manager local health returned ${local_status}"

  public_status=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
  [[ "$public_status" == 200 ]] || die "public root returned ${public_status}"

  log "validation=passed control-plane=healthy"
  runtime_diagnostics
  schema_diagnostics
}

restart_control_plane() {
  if [[ -x /usr/local/sbin/routegate-recovery ]]; then
    /usr/local/sbin/routegate-recovery restart-services
  elif [[ -x /usr/local/bin/routegate-recovery ]]; then
    /usr/local/bin/routegate-recovery restart-services
  else
    die "routegate-recovery is not installed"
  fi
  validate_platform
}

restart_runtime() {
  local label=$1
  local service=$2
  local load_state

  load_state=$(systemctl show --property=LoadState --value "$service" 2>/dev/null || true)
  [[ "$load_state" == loaded ]] || die "runtime is not installed or unmanaged: ${label}"

  if [[ "$label" == sing-box ]]; then
    command -v sing-box >/dev/null 2>&1 || die "sing-box binary is unavailable"
    [[ -r /etc/sing-box/config.json ]] || die "sing-box config is unavailable"
    sing-box check -c /etc/sing-box/config.json >/dev/null || die "sing-box config validation failed"
  fi

  systemctl restart "$service"
  for _ in $(seq 1 20); do
    systemctl is-active --quiet "$service" && break
    sleep 1
  done
  systemctl is-active --quiet "$service" || die "runtime failed to become active: ${label}"
  log "runtime-restart=passed runtime=${label} service=${service}"
  runtime_diagnostics
}

reconcile_mtproto_port_conflict() {
  local config_file=/etc/routegate-mtproto/config.toml
  local backup_file current_line current_port row vless_port shadowsocks_port target_port server_count tmp_file

  [[ -r /etc/routegate/manager.env ]] || die "manager environment is unavailable"
  [[ -r "$config_file" ]] || die "managed MTProto config is unavailable"
  command -v psql >/dev/null 2>&1 || die "psql is unavailable"

  set -a
  # shellcheck disable=SC1091
  source /etc/routegate/manager.env
  set +a
  [[ -n ${ROUTEGATE_DATABASE_URL:-} ]] || die "database URL is unavailable"

  server_count=$(psql "$ROUTEGATE_DATABASE_URL" -qAtc 'SELECT count(*) FROM servers' 2>/dev/null || true)
  [[ "$server_count" == 1 ]] || die "production-like port reconciliation requires exactly one managed server"

  row=$(psql "$ROUTEGATE_DATABASE_URL" -qAt -F '|' -c 'SELECT vless_port, shadowsocks_port, mtproto_port FROM servers LIMIT 1' 2>/dev/null || true)
  IFS='|' read -r vless_port shadowsocks_port target_port <<<"$row"
  [[ "$vless_port" =~ ^[0-9]+$ && "$shadowsocks_port" =~ ^[0-9]+$ && "$target_port" =~ ^[0-9]+$ ]] || die "database listener ports are unavailable"
  [[ "$target_port" != "$vless_port" && "$target_port" != "$shadowsocks_port" ]] || die "database still contains a TCP listener collision"

  current_line=$(grep -E '^bind-to = "0\.0\.0\.0:[0-9]{1,5}"$' "$config_file" || true)
  [[ $(printf '%s\n' "$current_line" | sed '/^$/d' | wc -l | tr -d ' ') == 1 ]] || die "managed MTProto bind-to line is ambiguous"
  current_port=$(printf '%s\n' "$current_line" | grep -Eo '[0-9]{1,5}' | tail -n 1)
  [[ "$current_port" =~ ^[0-9]+$ ]] || die "managed MTProto port is invalid"

  if [[ "$current_port" != "$target_port" ]]; then
    backup_file="${config_file}.routegate-port-reconcile.$(date -u +%Y%m%dT%H%M%SZ).bak"
    cp -a "$config_file" "$backup_file"
    tmp_file=$(mktemp /etc/routegate-mtproto/config.toml.XXXXXX)
    if ! awk -v port="$target_port" '
      BEGIN { replaced = 0 }
      /^bind-to = "0\.0\.0\.0:[0-9]+"$/ {
        print "bind-to = \"0.0.0.0:" port "\""
        replaced = 1
        next
      }
      { print }
      END { if (replaced != 1) exit 42 }
    ' "$config_file" >"$tmp_file"; then
      rm -f "$tmp_file"
      die "failed to render reconciled MTProto config"
    fi
    chmod --reference="$config_file" "$tmp_file"
    chown --reference="$config_file" "$tmp_file"
    mv "$tmp_file" "$config_file"

    if ! systemctl restart routegate-mtproto.service; then
      cp -a "$backup_file" "$config_file"
      systemctl restart routegate-mtproto.service || true
      die "MTProto failed after port reconciliation; previous config restored"
    fi
    for _ in $(seq 1 20); do
      systemctl is-active --quiet routegate-mtproto.service && break
      sleep 1
    done
    if ! systemctl is-active --quiet routegate-mtproto.service; then
      cp -a "$backup_file" "$config_file"
      systemctl restart routegate-mtproto.service || true
      die "MTProto did not become active after port reconciliation; previous config restored"
    fi
    log "mtproto-port-reconcile=passed old-port=${current_port} new-port=${target_port}"
  else
    log "mtproto-port-reconcile=already-current port=${target_port}"
  fi

  command -v sing-box >/dev/null 2>&1 || die "sing-box binary is unavailable"
  [[ -r /etc/sing-box/config.json ]] || die "sing-box config is unavailable"
  sing-box check -c /etc/sing-box/config.json >/dev/null || die "sing-box config validation failed"
  systemctl restart sing-box.service
  for _ in $(seq 1 20); do
    systemctl is-active --quiet sing-box.service && break
    sleep 1
  done
  systemctl is-active --quiet sing-box.service || die "sing-box did not become active after MTProto port reconciliation"
  log "sing-box-recovery=passed"
  runtime_diagnostics
  validate_platform
}

renew_certificate() {
  if [[ -x /usr/local/sbin/routegate-recovery ]]; then
    /usr/local/sbin/routegate-recovery renew-certificate
  elif [[ -x /usr/local/bin/routegate-recovery ]]; then
    /usr/local/bin/routegate-recovery renew-certificate
  else
    die "routegate-recovery is not installed"
  fi
  log "certificate-renewal=passed"
}

main() {
  require_root
  case "$OPERATION" in
    diagnose)
      diagnose
      ;;
    diagnose-sing-box)
      sing_box_diagnostics
      ;;
    validate)
      validate_platform
      ;;
    restart-control-plane)
      restart_control_plane
      ;;
    restart-sing-box)
      restart_runtime sing-box sing-box.service
      ;;
    restart-wireguard)
      restart_runtime wireguard wg-quick@routegate-wg0.service
      ;;
    restart-hysteria2)
      restart_runtime hysteria2 hysteria-server.service
      ;;
    restart-mtproto)
      restart_runtime mtproto routegate-mtproto.service
      ;;
    reconcile-mtproto-port-conflict)
      reconcile_mtproto_port_conflict
      ;;
    renew-certificate)
      renew_certificate
      ;;
    *)
      die "unsupported operation"
      ;;
  esac
}

main "$@"
