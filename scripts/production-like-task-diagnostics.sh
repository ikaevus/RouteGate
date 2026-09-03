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

classify_journals() {
  local agent_journal manager_journal agent_processes agent_pid agent_restarts
  agent_journal=$(journalctl -u routegate-agent.service --since '-90 minutes' -n 1800 --no-pager -o cat 2>/dev/null || true)
  manager_journal=$(journalctl -u routegate-manager.service --since '-90 minutes' -n 1800 --no-pager -o cat 2>/dev/null || true)
  agent_processes=$(pgrep -xc routegate-agent 2>/dev/null || true)
  agent_pid=$(systemctl show routegate-agent.service --property=MainPID --value 2>/dev/null || true)
  agent_restarts=$(systemctl show routegate-agent.service --property=NRestarts --value 2>/dev/null || true)

  log "agent process-count=${agent_processes:-unknown} main-pid-present=$([[ ${agent_pid:-0} =~ ^[1-9][0-9]*$ ]] && printf true || printf false) restarts=${agent_restarts:-unknown} heartbeats=$(count_matches "$agent_journal" 'heartbeat accepted') process-task-failed=$(count_matches "$agent_journal" 'process agent task failed') completion-retry-exhausted=$(count_matches "$agent_journal" 'complete agent task after [0-9]+ attempts') http-404=$(count_matches "$agent_journal" 'status 404') http-4xx=$(count_matches "$agent_journal" 'status 4[0-9][0-9]') http-5xx=$(count_matches "$agent_journal" 'status 5[0-9][0-9]') context-timeout=$(count_matches "$agent_journal" 'context deadline exceeded|context canceled') connection-failure=$(count_matches "$agent_journal" 'connection refused|connection reset|broken pipe|no route to host')"
  log "manager complete-config-failed=$(count_matches "$manager_journal" 'complete agent config task failed') complete-operation-failed=$(count_matches "$manager_journal" 'complete agent operation task failed') database-error=$(count_matches "$manager_journal" 'database_error|database error') task-not-found=$(count_matches "$manager_journal" 'task_not_found|task not found')"
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
      END
    FROM config_apply_jobs
    ORDER BY created_at DESC
    LIMIT 1
  " 2>/dev/null || true)
  if [[ -n "$latest" ]]; then
    local status duration age components payload_size error_class
    IFS='|' read -r status duration age components payload_size error_class <<<"$latest"
    log "latest-config-job status=${status:-unknown} duration-seconds=${duration:--1} age-seconds=${age:--1} result-components=${components:-0} result-payload-bytes=${payload_size:-0} error-class=${error_class:-unknown}"
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
  database_diagnostics
}

main "$@"
