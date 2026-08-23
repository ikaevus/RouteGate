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

sing_box_diagnostics() {
  service_state sing-box sing-box.service
  command -v sing-box >/dev/null 2>&1 || die "sing-box binary is unavailable"
  [[ -r /etc/sing-box/config.json ]] || die "sing-box config is unavailable"

  local version config_state unit_execstart
  version=$(sing-box version 2>/dev/null | head -n 1 || true)
  if sing-box check -c /etc/sing-box/config.json >/dev/null 2>&1; then
    config_state=valid
  else
    config_state=invalid
  fi
  unit_execstart=$(systemctl show --property=ExecStart --value sing-box.service 2>/dev/null || true)
  unit_execstart=${unit_execstart//$'\n'/ }
  unit_execstart=${unit_execstart:0:240}

  log "runtime=sing-box version=${version:-unknown}"
  log "runtime=sing-box config=${config_state}"
  log "runtime=sing-box unit-execstart=${unit_execstart:-unknown}"

  # Deliberately expose only systemd metadata and command/config compatibility.
  # Raw journal/config output remains outside the bridge because it may contain
  # connection material or other deployment-sensitive values.
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
    renew-certificate)
      renew_certificate
      ;;
    *)
      die "unsupported operation"
      ;;
  esac
}

main "$@"
