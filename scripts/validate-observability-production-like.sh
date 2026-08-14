#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

EXPECTED_COMMIT=${1:-}
MANAGER_ENV=${ROUTEGATE_MANAGER_ENV:-/etc/routegate/manager.env}
API=${ROUTEGATE_MANAGER_API:-http://127.0.0.1:8080}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}
WORK_DIR=$(mktemp -d /tmp/routegate-observability-validation.XXXXXX)
ENV_BACKUP="$WORK_DIR/manager.env"
ENABLED_RECIPIENTS="$WORK_DIR/enabled-recipients.txt"
SESSION_ID=""
TOKEN=""
SERVER_ID=""
AGENT_ID=""
DIAGNOSTIC_RUN_ID=""
DIAGNOSTIC_JOB_ID=""
VALIDATION_STARTED_AT=""
MANAGER_ENV_MUTATED=0
RECIPIENTS_MUTATED=0

log() {
  printf '[RG-113I] %s\n' "$*"
}

fail() {
  printf '[RG-113I] ERROR: %s\n' "$*" >&2
  return 1
}

wait_manager() {
  local attempts=${1:-30}
  local i
  for ((i=0; i<attempts; i++)); do
    if curl -fsS "$API/api/admin/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail "Manager did not become ready."
}

wait_service_active() {
  local service=$1
  local attempts=${2:-30}
  local i
  for ((i=0; i<attempts; i++)); do
    if systemctl is-active --quiet "$service"; then
      return 0
    fi
    sleep 1
  done
  fail "$service did not become active."
}

wait_health_state() {
  local check_key=$1
  local expected_state=$2
  local attempts=${3:-40}
  local sleep_seconds=${4:-5}
  local i state
  for ((i=0; i<attempts; i++)); do
    state=$(psql "$DB_URL" -qAtc "
      SELECT state
      FROM observability_current_health
      WHERE resource_type='server'
        AND resource_id='${SERVER_ID}'
        AND check_key='${check_key}'
    " || true)
    if [[ "$state" == "$expected_state" ]]; then
      return 0
    fi
    sleep "$sleep_seconds"
  done
  fail "Health check ${check_key} did not reach ${expected_state}; last state=${state:-missing}."
}

wait_alert_state() {
  local fingerprint=$1
  local expected_state=$2
  local attempts=${3:-40}
  local sleep_seconds=${4:-5}
  local i state
  for ((i=0; i<attempts; i++)); do
    state=$(psql "$DB_URL" -qAtc "
      SELECT condition_state
      FROM observability_alerts
      WHERE fingerprint='${fingerprint}'
      ORDER BY started_at DESC, id DESC
      LIMIT 1
    " || true)
    if [[ "$state" == "$expected_state" ]]; then
      return 0
    fi
    sleep "$sleep_seconds"
  done
  fail "Alert ${fingerprint} did not reach ${expected_state}; last state=${state:-missing}."
}

latest_alert_id() {
  local fingerprint=$1
  psql "$DB_URL" -qAtc "
    SELECT id::text
    FROM observability_alerts
    WHERE fingerprint='${fingerprint}'
    ORDER BY started_at DESC, id DESC
    LIMIT 1
  "
}

intent_count_for_alert() {
  local alert_id=$1
  psql "$DB_URL" -qAtc "
    SELECT COUNT(*)
    FROM observability_notification_intents
    WHERE alert_id='${alert_id}'::uuid
  "
}

restore_validation_state() {
  local rc=$?
  trap - EXIT ERR
  set +e

  log "Restoring validation state."
  systemctl start sing-box >/dev/null 2>&1 || true
  systemctl start routegate-manager >/dev/null 2>&1 || true
  systemctl start routegate-agent >/dev/null 2>&1 || true

  if [[ "$MANAGER_ENV_MUTATED" == "1" && -s "$ENV_BACKUP" ]]; then
    install -m 0600 "$ENV_BACKUP" "$MANAGER_ENV"
    systemctl restart routegate-manager >/dev/null 2>&1 || true
    wait_manager 30 >/dev/null 2>&1 || true
  fi

  if [[ "$RECIPIENTS_MUTATED" == "1" && -n "${DB_URL:-}" && -s "$ENABLED_RECIPIENTS" ]]; then
    while IFS= read -r recipient_id; do
      [[ -n "$recipient_id" ]] || continue
      psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \
        "UPDATE delivery_recipients SET enabled=TRUE, updated_at=now() WHERE id='${recipient_id}'::uuid" >/dev/null 2>&1 || true
    done < "$ENABLED_RECIPIENTS"
  fi

  if [[ -n "${SESSION_ID:-}" && -n "${DB_URL:-}" ]]; then
    psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \
      "DELETE FROM auth_sessions WHERE id='${SESSION_ID}'::uuid" >/dev/null 2>&1 || true
  fi

  if [[ -n "${DB_URL:-}" && -n "${DIAGNOSTIC_RUN_ID:-}" ]]; then
    psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \
      "DELETE FROM observability_diagnostic_runs WHERE id='${DIAGNOSTIC_RUN_ID}'::uuid" >/dev/null 2>&1 || true
  fi
  if [[ -n "${DB_URL:-}" && -n "${DIAGNOSTIC_JOB_ID:-}" ]]; then
    psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc \
      "DELETE FROM agent_operation_jobs WHERE id='${DIAGNOSTIC_JOB_ID}'::uuid" >/dev/null 2>&1 || true
  fi

  if [[ -n "${DB_URL:-}" && -n "${SERVER_ID:-}" && -n "${VALIDATION_STARTED_AT:-}" ]]; then
    psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc "
      DELETE FROM observability_alerts
      WHERE resource_type='server'
        AND resource_id='${SERVER_ID}'
        AND started_at >= '${VALIDATION_STARTED_AT}'::timestamptz
        AND rule_key IN ('agent.telemetry.freshness','vpn_core.service');
      DELETE FROM observability_health_transitions
      WHERE resource_type='server'
        AND resource_id='${SERVER_ID}'
        AND observed_at >= '${VALIDATION_STARTED_AT}'::timestamptz
        AND check_key IN ('agent.telemetry.freshness','vpn_core.service');
    " >/dev/null 2>&1 || true
  fi

  rm -rf "$WORK_DIR"
  exit "$rc"
}

trap restore_validation_state EXIT ERR

[[ $EUID -eq 0 ]] || fail "This validation must run as root."
[[ -r "$MANAGER_ENV" ]] || fail "Manager environment is not readable: $MANAGER_ENV"
for command_name in curl openssl psql python3 sha256sum systemctl; do
  command -v "$command_name" >/dev/null || fail "Required command is missing: $command_name"
done
for service in routegate-manager routegate-agent sing-box; do
  systemctl is-active --quiet "$service" || fail "$service must be active before validation."
done

cp -a "$MANAGER_ENV" "$ENV_BACKUP"
set -a
# shellcheck disable=SC1090
source "$MANAGER_ENV"
set +a
DB_URL=${ROUTEGATE_DATABASE_URL:?ROUTEGATE_DATABASE_URL is required}
VALIDATION_STARTED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)

log "Validating schema and current runtime."
LATEST_SCHEMA=$(psql "$DB_URL" -qAtc "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1")
[[ "$LATEST_SCHEMA" == "000123_server_geography" ]] || fail "Applied schema is ${LATEST_SCHEMA}, want 000123_server_geography."

for migration in \
  000118_observability_foundation \
  000119_observability_agent_telemetry \
  000120_observability_alert_recovery \
  000121_observability_notification_outbox \
  000122_observability_diagnostics \
  000123_server_geography; do
  applied=$(psql "$DB_URL" -qAtc "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version='${migration}')")
  [[ "$applied" == "t" ]] || fail "Migration ${migration} is not applied."
done

for _ in $(seq 1 30); do
  runtime_row=$(psql "$DB_URL" -qAtc "
    SELECT t.server_id::text || '|' || t.agent_id::text
    FROM observability_agent_telemetry t
    JOIN agents a ON a.id=t.agent_id
    WHERE t.received_at >= now() - interval '90 seconds'
      AND a.status <> 'disabled'
    ORDER BY t.received_at DESC
    LIMIT 1
  " || true)
  [[ -n "$runtime_row" ]] && break
  sleep 2
done
[[ -n "${runtime_row:-}" ]] || fail "No fresh Agent telemetry was found."
SERVER_ID=${runtime_row%%|*}
AGENT_ID=${runtime_row##*|}

wait_health_state agent.telemetry.freshness healthy 20 2
wait_health_state vpn_core.service healthy 20 2

ADMIN_ID=$(psql "$DB_URL" -qAtc "
  SELECT u.id::text
  FROM users u
  JOIN user_roles ur ON ur.user_id=u.id
  JOIN roles r ON r.id=ur.role_id
  WHERE r.code='super_admin' AND u.status='active'
  ORDER BY u.created_at
  LIMIT 1
")
[[ -n "$ADMIN_ID" ]] || fail "No active SuperAdmin exists for local API validation."
TOKEN=$(openssl rand -hex 32)
TOKEN_HASH=$(printf '%s' "$TOKEN" | sha256sum | awk '{print $1}')
SESSION_ID=$(psql "$DB_URL" -qAtc "
  INSERT INTO auth_sessions (user_id, token_hash, expires_at, user_agent)
  VALUES ('${ADMIN_ID}'::uuid, '${TOKEN_HASH}', now() + interval '20 minutes', 'RG-113I production-like validation')
  RETURNING id::text
")

curl -fsS -H "Authorization: Bearer $TOKEN" "$API/api/v1/system/version" > "$WORK_DIR/version.json"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API/api/v1/analytics/overview" > "$WORK_DIR/analytics.json"
python3 - "$WORK_DIR/version.json" "$WORK_DIR/analytics.json" "$EXPECTED_COMMIT" "$SERVER_ID" <<'PY_API'
import json
import sys
version_path, analytics_path, expected_commit, server_id = sys.argv[1:]
with open(version_path, encoding='utf-8') as f:
    version = json.load(f)
with open(analytics_path, encoding='utf-8') as f:
    analytics = json.load(f)
assert version['database']['expectedSchemaVersion'] == 123, version['database']
assert version['database']['appliedSchemaVersion'] == '000123_server_geography', version['database']
if expected_commit:
    assert version['manager']['gitCommit'] == expected_commit, version['manager']
nodes = analytics.get('nodes')
assert isinstance(nodes, list), analytics
node = next((item for item in nodes if item.get('id') == server_id), None)
assert node is not None, nodes
assert node.get('health', {}).get('state') in {'healthy', 'degraded', 'unhealthy', 'unknown'}, node
assert isinstance(node.get('agent', {}).get('observationFresh'), bool), node
print(f"Analytics read model validated for server {server_id}")
PY_API

log "Temporarily disabling Delivery recipients so failure tests cannot notify real destinations."
psql "$DB_URL" -qAtc "SELECT id::text FROM delivery_recipients WHERE enabled=TRUE ORDER BY id" > "$ENABLED_RECIPIENTS"
psql "$DB_URL" -v ON_ERROR_STOP=1 -qAtc "UPDATE delivery_recipients SET enabled=FALSE, updated_at=now() WHERE enabled=TRUE" >/dev/null
RECIPIENTS_MUTATED=1

log "Validating Agent loss -> stale health -> one canonical firing alert -> recovery."
AGENT_FINGERPRINT="agent.telemetry.freshness:server:${SERVER_ID}"
systemctl stop routegate-agent
wait_health_state agent.telemetry.freshness unknown 45 4
wait_alert_state "$AGENT_FINGERPRINT" firing 30 4
AGENT_ALERT_ID=$(latest_alert_id "$AGENT_FINGERPRINT")
[[ -n "$AGENT_ALERT_ID" ]] || fail "Agent freshness alert was not created."
[[ "$(psql "$DB_URL" -qAtc "SELECT COUNT(*) FROM observability_alerts WHERE fingerprint='${AGENT_FINGERPRINT}' AND condition_state IN ('pending','firing')")" == "1" ]] \
  || fail "Agent loss produced duplicate active alerts."
[[ "$(intent_count_for_alert "$AGENT_ALERT_ID")" == "1" ]] \
  || fail "Agent firing alert must create exactly one notification intent before recovery."

systemctl start routegate-agent
wait_service_active routegate-agent 30
wait_health_state agent.telemetry.freshness healthy 45 3
wait_alert_state "$AGENT_FINGERPRINT" resolved 45 3
[[ "$(intent_count_for_alert "$AGENT_ALERT_ID")" == "2" ]] \
  || fail "Agent alert must have exactly firing + resolved notification intents."

log "Validating VPN Core failure, Manager restart idempotency, and recovery."
CORE_FINGERPRINT="vpn_core.service:server:${SERVER_ID}"
systemctl stop sing-box
wait_health_state vpn_core.service unhealthy 45 3
wait_alert_state "$CORE_FINGERPRINT" firing 30 3
CORE_ALERT_ID=$(latest_alert_id "$CORE_FINGERPRINT")
[[ -n "$CORE_ALERT_ID" ]] || fail "VPN Core alert was not created."
CORE_INTENTS_BEFORE=$(intent_count_for_alert "$CORE_ALERT_ID")
[[ "$CORE_INTENTS_BEFORE" == "1" ]] || fail "VPN Core firing must create exactly one notification intent."

systemctl restart routegate-manager
wait_manager 30
sleep 35
[[ "$(latest_alert_id "$CORE_FINGERPRINT")" == "$CORE_ALERT_ID" ]] \
  || fail "Manager restart created a different active VPN Core alert episode."
[[ "$(psql "$DB_URL" -qAtc "SELECT COUNT(*) FROM observability_alerts WHERE fingerprint='${CORE_FINGERPRINT}' AND condition_state='firing'")" == "1" ]] \
  || fail "Manager restart duplicated the firing VPN Core alert."
[[ "$(intent_count_for_alert "$CORE_ALERT_ID")" == "$CORE_INTENTS_BEFORE" ]] \
  || fail "Manager restart duplicated the firing notification intent."

systemctl start sing-box
wait_service_active sing-box 30
wait_health_state vpn_core.service healthy 45 3
wait_alert_state "$CORE_FINGERPRINT" resolved 45 3
[[ "$(intent_count_for_alert "$CORE_ALERT_ID")" == "2" ]] \
  || fail "VPN Core alert must have exactly firing + resolved notification intents."

log "Validating typed host_overview diagnostics through the real Agent task queue."
DIAGNOSTIC_JOB_ID=$(psql "$DB_URL" -qAtc "
  WITH candidate_agent AS (
    SELECT id
    FROM agents
    WHERE id='${AGENT_ID}'::uuid
      AND server_id='${SERVER_ID}'::uuid
      AND status <> 'disabled'
      AND capabilities @> '{\"diagnosticProfiles\":[\"host_overview\"]}'::jsonb
    LIMIT 1
  )
  INSERT INTO agent_operation_jobs (server_id, agent_id, kind, operation)
  SELECT '${SERVER_ID}'::uuid, id, 'diagnostic', 'host_overview'
  FROM candidate_agent
  RETURNING id::text
")
[[ -n "$DIAGNOSTIC_JOB_ID" ]] || fail "Agent did not advertise host_overview diagnostics."
DIAGNOSTIC_RUN_ID=$(psql "$DB_URL" -qAtc "
  INSERT INTO observability_diagnostic_runs (server_id, agent_operation_job_id, profile_key)
  VALUES ('${SERVER_ID}'::uuid, '${DIAGNOSTIC_JOB_ID}'::uuid, 'host_overview')
  RETURNING id::text
")

for _ in $(seq 1 45); do
  diagnostic_state=$(psql "$DB_URL" -qAtc "
    SELECT status || '|' || COALESCE(state,'') || '|' || COALESCE(reason_code,'') || '|' || COALESCE(recommended_action,'')
    FROM observability_diagnostic_runs
    WHERE id='${DIAGNOSTIC_RUN_ID}'::uuid
  " || true)
  case "$diagnostic_state" in
    succeeded\|*) break ;;
    failed\|*) fail "Typed diagnostic failed: ${diagnostic_state}" ;;
  esac
  sleep 2
done
[[ "$diagnostic_state" == succeeded\|* ]] || fail "Typed diagnostic did not complete."
DIAGNOSTIC_PAYLOAD_TYPE=$(psql "$DB_URL" -qAtc "SELECT jsonb_typeof(result_payload) FROM observability_diagnostic_runs WHERE id='${DIAGNOSTIC_RUN_ID}'::uuid")
[[ "$DIAGNOSTIC_PAYLOAD_TYPE" == "object" ]] || fail "Diagnostic result payload is not structured JSON."

log "Validating authenticated Prometheus surfaces without requiring a Prometheus server."
MONITORING_TOKEN=$(openssl rand -hex 32)
python3 - "$MANAGER_ENV" "$MONITORING_TOKEN" <<'PY_ENV'
import sys
path, token = sys.argv[1:]
with open(path, encoding='utf-8') as f:
    lines = f.read().splitlines()
values = {
    'ROUTEGATE_MONITORING_ENABLED': 'true',
    'ROUTEGATE_MONITORING_TOKEN': token,
}
out = []
seen = set()
for line in lines:
    key = line.split('=', 1)[0].strip() if '=' in line else ''
    if key in values:
        out.append(f'{key}="{values[key]}"')
        seen.add(key)
    else:
        out.append(line)
for key, value in values.items():
    if key not in seen:
        out.append(f'{key}="{value}"')
with open(path, 'w', encoding='utf-8') as f:
    f.write('\n'.join(out) + '\n')
PY_ENV
chmod 0600 "$MANAGER_ENV"
MANAGER_ENV_MUTATED=1
systemctl restart routegate-manager
wait_manager 30

unauthorized_code=$(curl -sS -o /dev/null -w '%{http_code}' "$API/metrics")
[[ "$unauthorized_code" == "401" ]] || fail "Monitoring endpoint without token returned ${unauthorized_code}, want 401."
curl -fsS -H "Authorization: Bearer $MONITORING_TOKEN" "$API/metrics" > "$WORK_DIR/metrics.txt"
curl -fsS -H "Authorization: Bearer $MONITORING_TOKEN" "$API/metrics/fleet" > "$WORK_DIR/fleet.txt"
grep -q '^routegate_manager_up 1$' "$WORK_DIR/metrics.txt" || fail "Manager metrics do not report routegate_manager_up=1."
grep -q '^routegate_postgresql_up 1$' "$WORK_DIR/metrics.txt" || fail "Manager metrics do not report PostgreSQL up."
grep -q "routegate_agent_observation_fresh{server_id=\"${SERVER_ID}\"} 1" "$WORK_DIR/fleet.txt" \
  || fail "Fleet metrics do not report a fresh Agent observation."
grep -q "routegate_server_health{server_id=\"${SERVER_ID}\",state=\"healthy\"} 1" "$WORK_DIR/fleet.txt" \
  || fail "Fleet metrics do not report healthy aggregate server state."

install -m 0600 "$ENV_BACKUP" "$MANAGER_ENV"
MANAGER_ENV_MUTATED=0
systemctl restart routegate-manager
wait_manager 30

log "Validating final service and public health."
for service in routegate-manager routegate-agent sing-box; do
  systemctl is-active --quiet "$service" || fail "$service is not active after recovery."
done
/usr/bin/sing-box check -c /etc/sing-box/config.json >/dev/null
public_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
[[ "$public_status" == "200" ]] || fail "Public site returned HTTP ${public_status}."
wait_health_state agent.telemetry.freshness healthy 20 2
wait_health_state vpn_core.service healthy 20 2

log "RG-113I observability production-like validation PASSED"
log "server_id=${SERVER_ID}"
log "agent_alert=${AGENT_ALERT_ID}"
log "vpn_core_alert=${CORE_ALERT_ID}"
log "diagnostic=${DIAGNOSTIC_RUN_ID}"
