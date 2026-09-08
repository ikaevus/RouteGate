#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

log() {
  printf '[routegate-task-diagnostics] %s\n' "$*"
}

require_root() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || { log 'ERROR: must run as root'; exit 1; }
}

count_matches() {
  local payload=$1
  local pattern=$2
  printf '%s\n' "$payload" | grep -Eic "$pattern" || true
}

classify_process_failure() {
  local line=${1:-}
  case "$line" in
    *"rendered config selects an unsupported VPN Core adapter"*|*"rendered config contains no managed VPN Core adapters"*) printf 'select' ;;
    *"task id is required"*|*"config version id is required"*|*"rendered config envelope"*|*"create config staging dir"*|*"write staged config"*|*"commit staged config"*) printf 'stage' ;;
    *"sing-box check"*|*"check timed out"*) printf 'validate' ;;
    *"apply VPN runtime config"*) printf 'apply' ;;
    *"restart VPN runtime"*|*"systemctl restart"*|*"enable service before restart"*) printf 'restart' ;;
    *"VPN runtime active check"*|*"systemctl is-active"*) printf 'healthcheck' ;;
    *"VPN runtime persistence check"*|*"systemctl is-enabled"*) printf 'persistence' ;;
    *"VPN runtime listener healthcheck"*|*"listener on port"*|*"contains no managed TCP listener"*) printf 'listener' ;;
    *"complete agent task"*|*"report failure:"*|*"/api/v1/agent/tasks/"*"/result"*) printf 'completion' ;;
    *"unsupported agent task kind"*) printf 'dispatch' ;;
    *) printf 'unknown' ;;
  esac
}

classify_presence_failure() {
  local line=${1:-}
  case "$line" in
    *"status 400"*"unknown, inactive, or foreign VPN account"*) printf 'unknown-account' ;;
    *"status 400"*"observedAt is outside"*) printf 'clock-window' ;;
    *"status 400"*"duplicate account and protocol"*) printf 'duplicate-account-protocol' ;;
    *"status 400"*"fields are required"*) printf 'missing-fields' ;;
    *"status 400"*"connectionCount"*) printf 'invalid-connection-count' ;;
    *"status 400"*"confidence"*) printf 'invalid-confidence' ;;
    *"status 400"*"valid JSON"*) printf 'invalid-json' ;;
    *"status 401"*) printf 'unauthorized' ;;
    *"status 403"*) printf 'forbidden' ;;
    *"status 404"*) printf 'endpoint-not-found' ;;
    *"status 4"[0-9][0-9]*) printf 'other-4xx' ;;
    *"status 5"[0-9][0-9]*) printf 'manager-5xx' ;;
    *"context deadline exceeded"*|*"context canceled"*) printf 'timeout' ;;
    *"connection refused"*|*"connection reset"*|*"no route to host"*) printf 'connection' ;;
    *"parse client presence file"*) printf 'fallback-json' ;;
    *"read client presence file"*|*"stat client presence file"*) printf 'fallback-file' ;;
    *"read active sing-box config"*|*"parse active sing-box config"*) printf 'sing-box-config' ;;
    *"read sing-box process ID"*|*"read sing-box journal"*|*"read sing-box presence log"*|*"read established TCP sockets"*) printf 'sing-box-probe' ;;
    *"read active WireGuard config"*) printf 'wireguard-config' ;;
    *"executable file not found"*|*"exec format error"*) printf 'system-command' ;;
    *"permission denied"*) printf 'permission' ;;
    *"unexpected end of JSON input"*|*"unexpected EOF"*|*" EOF"*) printf 'empty-response' ;;
    '') printf 'none' ;;
    *) printf 'other' ;;
  esac
}

classify_journals() {
  local agent_journal manager_journal agent_processes agent_pid agent_restarts last_failure last_failure_class last_presence_failure last_presence_failure_class latest_presence_accepted
  agent_journal=$(journalctl -u routegate-agent.service --since '-90 minutes' -n 1800 --no-pager -o cat 2>/dev/null || true)
  manager_journal=$(journalctl -u routegate-manager.service --since '-90 minutes' -n 1800 --no-pager -o cat 2>/dev/null || true)
  agent_processes=$(pgrep -xc routegate-agent 2>/dev/null || true)
  agent_pid=$(systemctl show routegate-agent.service --property=MainPID --value 2>/dev/null || true)
  agent_restarts=$(systemctl show routegate-agent.service --property=NRestarts --value 2>/dev/null || true)
  last_failure=$(printf '%s\n' "$agent_journal" | grep -Ei 'process agent task failed' | tail -n 1 || true)
  last_failure_class=$(classify_process_failure "$last_failure")
  last_presence_failure=$(printf '%s\n' "$agent_journal" | grep -Ei 'report client presence failed' | tail -n 1 || true)
  last_presence_failure_class=$(classify_presence_failure "$last_presence_failure")
  latest_presence_accepted=$(printf '%s\n' "$agent_journal" | grep -Ei 'client presence report accepted' | tail -n 1 | sed -n 's/.*accepted[= ]\([0-9][0-9]*\).*/\1/p' || true)

  log "agent process-count=${agent_processes:-unknown} main-pid-present=$([[ ${agent_pid:-0} =~ ^[1-9][0-9]*$ ]] && printf true || printf false) restarts=${agent_restarts:-unknown} heartbeats=$(count_matches "$agent_journal" 'heartbeat accepted') process-task-failed=$(count_matches "$agent_journal" 'process agent task failed') completion-retry-exhausted=$(count_matches "$agent_journal" 'complete agent task after [0-9]+ attempts') http-404=$(count_matches "$agent_journal" 'status 404') http-4xx=$(count_matches "$agent_journal" 'status 4[0-9][0-9]') http-5xx=$(count_matches "$agent_journal" 'status 5[0-9][0-9]') context-timeout=$(count_matches "$agent_journal" 'context deadline exceeded|context canceled') connection-failure=$(count_matches "$agent_journal" 'connection refused|connection reset|broken pipe|no route to host')"
  log "agent failure-stage last=${last_failure_class:-none} select=$(count_matches "$agent_journal" 'rendered config selects an unsupported VPN Core adapter|rendered config contains no managed VPN Core adapters') stage=$(count_matches "$agent_journal" 'task id is required|config version id is required|rendered config envelope|create config staging dir|write staged config|commit staged config') validate=$(count_matches "$agent_journal" 'sing-box check|check timed out') apply=$(count_matches "$agent_journal" 'apply VPN runtime config') restart=$(count_matches "$agent_journal" 'restart VPN runtime|systemctl restart|enable service before restart') healthcheck=$(count_matches "$agent_journal" 'VPN runtime active check|systemctl is-active') persistence=$(count_matches "$agent_journal" 'VPN runtime persistence check|systemctl is-enabled') listener=$(count_matches "$agent_journal" 'VPN runtime listener healthcheck|listener on port|contains no managed TCP listener') completion=$(count_matches "$agent_journal" 'complete agent task|report failure:|/api/v1/agent/tasks/.*/result')"
  log "client-presence failures=$(count_matches "$agent_journal" 'report client presence failed') last-class=${last_presence_failure_class:-none} accepted-reports=$(count_matches "$agent_journal" 'client presence report accepted') latest-accepted-items=${latest_presence_accepted:-unknown}"
  log "manager complete-config-failed=$(count_matches "$manager_journal" 'complete agent config task failed') complete-operation-failed=$(count_matches "$manager_journal" 'complete agent operation task failed') database-error=$(count_matches "$manager_journal" 'database_error|database error') task-not-found=$(count_matches "$manager_journal" 'task_not_found|task not found')"
}

sing_box_config_diagnostics() {
  local config=/etc/sing-box/config.json
  if [[ ! -r "$config" ]]; then
    log 'active-sing-box-config=unavailable'
    return 0
  fi

  local vless_count shadowsocks_count vless_users mtime now age
  vless_count=$(grep -Eoc '"type"[[:space:]]*:[[:space:]]*"vless"' "$config" 2>/dev/null || true)
  shadowsocks_count=$(grep -Eoc '"type"[[:space:]]*:[[:space:]]*"shadowsocks"' "$config" 2>/dev/null || true)
  vless_users=$(grep -Eo '"uuid"[[:space:]]*:' "$config" 2>/dev/null | wc -l | tr -d ' ')
  mtime=$(stat -c %Y "$config" 2>/dev/null || true)
  now=$(date +%s)
  age=-1
  if [[ "$mtime" =~ ^[0-9]+$ ]] && (( now >= mtime )); then
    age=$((now - mtime))
  fi

  log "active-sing-box-config vless=${vless_count:-0} vless-users=${vless_users:-0} shadowsocks=${shadowsocks_count:-0} mtime-age-seconds=${age}"
}

sing_box_presence_diagnostics() {
	local service=sing-box.service pid journal presence_log log_data log_state log_size log_mtime now log_age config_output
  pid=$(systemctl show "$service" --property=MainPID --value 2>/dev/null || true)
  if [[ ! ${pid:-} =~ ^[1-9][0-9]*$ ]]; then
    log 'sing-box-presence process=unavailable'
    return 0
  fi

	journal=$(journalctl -b -u "$service" --since '-15 minutes' -n 3000 --no-pager -o cat 2>/dev/null || true)
	log "sing-box-presence journal-lines=$(printf '%s\n' "$journal" | sed '/^[[:space:]]*$/d' | wc -l | tr -d ' ') vless-lines=$(count_matches "$journal" 'inbound/vless\[') connection-from=$(count_matches "$journal" 'inbound connection from') named-connection=$(count_matches "$journal" '\[[^]]+\][[:space:]]+inbound (multiplex |packet addr |packet )?connection') anonymous-connection-to=$(count_matches "$journal" 'inbound connection to') ansi-lines=$(printf '%s\n' "$journal" | LC_ALL=C grep -c $'\033\\[' || true)"

	presence_log=/var/lib/sing-box/routegate-presence.log
	config_output=absent
	if grep -Eq '"output"[[:space:]]*:[[:space:]]*"/var/lib/sing-box/routegate-presence\.log"' /etc/sing-box/config.json 2>/dev/null; then
		config_output=configured
	fi
	log_state=absent
	log_size=0
	log_age=-1
	log_data=''
	if [[ -r "$presence_log" ]]; then
		log_state=readable
		log_size=$(stat -c %s "$presence_log" 2>/dev/null || true)
		log_mtime=$(stat -c %Y "$presence_log" 2>/dev/null || true)
		now=$(date +%s)
		if [[ "$log_mtime" =~ ^[0-9]+$ ]] && (( now >= log_mtime )); then
			log_age=$((now - log_mtime))
		fi
		log_data=$(tail -c 8388608 "$presence_log" 2>/dev/null || true)
	fi
	log "sing-box-presence-file config-output=$config_output state=$log_state size-bytes=${log_size:-0} mtime-age-seconds=$log_age lines=$(printf '%s\n' "$log_data" | sed '/^[[:space:]]*$/d' | wc -l | tr -d ' ') vless-lines=$(count_matches "$log_data" 'inbound/vless\[') connection-from=$(count_matches "$log_data" 'inbound connection from') named-connection=$(count_matches "$log_data" '\[[^]]+\][[:space:]]+inbound (multiplex |packet addr |packet )?connection') process-errors=$(count_matches "$log_data" 'process connection from') tls-handshake-errors=$(count_matches "$log_data" 'TLS handshake') invalid-user-errors=$(count_matches "$log_data" 'invalid user|unknown user|authentication failed|bad request') eof-errors=$(count_matches "$log_data" '(^|[^[:alpha:]])EOF([^[:alpha:]]|$)') reset-errors=$(count_matches "$log_data" 'connection reset|broken pipe') timeout-errors=$(count_matches "$log_data" 'timeout|deadline exceeded')"
}

staged_sing_box_diagnostics() {
  local config_version_id=${1:-}
  local staging_dir=/var/lib/routegate-agent/configs
  if [[ -z "$config_version_id" ]]; then
    log 'failed-job-staged-config=unknown reason=no-config-version'
    return 0
  fi

  local staged_path="${staging_dir}/${config_version_id}.json"
  if [[ ! -r "$staged_path" ]]; then
    log 'failed-job-staged-config=absent'
    return 0
  fi

  local vless_count shadowsocks_count mtime now age
  vless_count=$(grep -Eoc '"type"[[:space:]]*:[[:space:]]*"vless"' "$staged_path" 2>/dev/null || true)
  shadowsocks_count=$(grep -Eoc '"type"[[:space:]]*:[[:space:]]*"shadowsocks"' "$staged_path" 2>/dev/null || true)
  mtime=$(stat -c %Y "$staged_path" 2>/dev/null || true)
  now=$(date +%s)
  age=-1
  if [[ "$mtime" =~ ^[0-9]+$ ]] && (( now >= mtime )); then
    age=$((now - mtime))
  fi

  log "failed-job-staged-config=present vless=${vless_count:-0} shadowsocks=${shadowsocks_count:-0} mtime-age-seconds=${age}"
}

load_manager_database() {
  [[ -r /etc/routegate/manager.env ]] || return 1
  command -v psql >/dev/null 2>&1 || return 1
  set -a
  # shellcheck disable=SC1091
  source /etc/routegate/manager.env
  set +a
  [[ -n ${ROUTEGATE_DATABASE_URL:-} ]]
}

database_diagnostics() {
  if ! load_manager_database; then
    log 'database=unavailable'
    return 0
  fi

  local latest audit_rows
  latest=$(psql "$ROUTEGATE_DATABASE_URL" -qAt -F '|' -c "
    SELECT
      status,
      COALESCE(floor(extract(epoch FROM (completed_at - started_at)))::bigint, -1),
      COALESCE(floor(extract(epoch FROM (now() - created_at)))::bigint, -1),
      COALESCE(jsonb_array_length(COALESCE(result_payload->'components', '[]'::jsonb)), 0),
      length(COALESCE(result_payload::text, '')),
      CASE
        WHEN COALESCE(error_message, '') ILIKE '%completion was not confirmed%' THEN 'completion-unconfirmed'
        WHEN COALESCE(error_message, '') ILIKE '%listener%' THEN 'listener-health'
        WHEN COALESCE(error_message, '') ILIKE '%restart%' THEN 'restart'
        WHEN COALESCE(error_message, '') ILIKE '%timeout%' THEN 'timeout'
        WHEN COALESCE(error_message, '') = '' THEN 'none'
        ELSE 'other'
      END,
      config_version_id::text
    FROM config_apply_jobs
    ORDER BY created_at DESC
    LIMIT 1
  " 2>/dev/null || true)
  if [[ -n "$latest" ]]; then
    local status duration age components payload_size error_class config_version_id
    IFS='|' read -r status duration age components payload_size error_class config_version_id <<<"$latest"
    log "latest-config-job status=${status:-unknown} duration-seconds=${duration:--1} age-seconds=${age:--1} result-components=${components:-0} result-payload-bytes=${payload_size:-0} error-class=${error_class:-unknown}"
    staged_sing_box_diagnostics "$config_version_id"
  else
    log 'latest-config-job=none'
  fi

  audit_rows=$(psql "$ROUTEGATE_DATABASE_URL" -qAt -F '|' -c "
    SELECT
      COALESCE(metadata->>'reason', 'unknown'),
      count(*)
    FROM audit_events
    WHERE action = 'agent.task.completion_rejected'
      AND created_at > now() - interval '90 minutes'
    GROUP BY COALESCE(metadata->>'reason', 'unknown')
    ORDER BY 1
  " 2>/dev/null || true)
  if [[ -z "$audit_rows" ]]; then
    log 'completion-rejected=0'
  else
    while IFS='|' read -r reason count; do
      [[ -n "$reason" ]] || continue
      log "completion-rejected reason=${reason} count=${count:-0}"
    done <<<"$audit_rows"
  fi

  local completed_count
  completed_count=$(psql "$ROUTEGATE_DATABASE_URL" -qAtc "
    SELECT count(*)
    FROM audit_events
    WHERE action = 'agent.task.completed'
      AND created_at > now() - interval '90 minutes'
  " 2>/dev/null || true)
  log "completion-audit-success=${completed_count:-unknown}"
}

main() {
  require_root
  classify_journals
  sing_box_config_diagnostics
  sing_box_presence_diagnostics
  database_diagnostics
}

main "$@"
