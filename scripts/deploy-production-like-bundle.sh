#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

EXPECTED_COMMIT=${1:?expected commit is required}
BUNDLE_FILE=${2:?bundle path is required}
EXPECTED_BUNDLE_SHA=${3:?bundle sha256 is required}
VALIDATION_SCRIPT=${4:?validation script path is required}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}
WORK_DIR=$(mktemp -d /tmp/routegate-production-like.XXXXXX)
BACKUP_DIR=""
DB_URL=""
EXPECTED_SCHEMA=""
MUTATED=0
DB_MAY_BE_MUTATED=0
STAGE=initializing

log() {
  printf '[production-like] %s\n' "$*"
}

cleanup() {
  rm -rf "$WORK_DIR"
  rm -f "$BUNDLE_FILE" "$VALIDATION_SCRIPT"
}

rollback() {
  local rc=$?
  local db_restore_rc=0
  trap - ERR
  set +e

  if [[ "$MUTATED" == "1" && -n "$BACKUP_DIR" ]]; then
    log "Failure at stage=${STAGE}; restoring production-like baseline."
    systemctl stop routegate-agent routegate-manager >/dev/null 2>&1 || true

    if [[ "$DB_MAY_BE_MUTATED" == "1" && -n "${DB_URL:-}" && -s "$BACKUP_DIR/routegate.pgdump" ]]; then
      pg_restore \
        --clean \
        --if-exists \
        --no-owner \
        --no-privileges \
        --exit-on-error \
        --dbname="$DB_URL" \
        "$BACKUP_DIR/routegate.pgdump" >/dev/null
      db_restore_rc=$?
      if (( db_restore_rc != 0 )); then
        printf 'WARNING: database restore failed (exit %d); continuing file/service rollback.\n' "$db_restore_rc" >&2
      fi
    fi

    install -m 0755 "$BACKUP_DIR/routegate-manager" /usr/local/bin/routegate-manager
    install -m 0755 "$BACKUP_DIR/routegate-agent" /usr/local/bin/routegate-agent

    rm -rf /opt/routegate-manager/migrations
    tar -xzf "$BACKUP_DIR/manager-migrations.tar.gz" -C /opt
    chown -R routegate:routegate /opt/routegate-manager

    rm -rf /var/www/routegate
    tar -xzf "$BACKUP_DIR/frontend.tar.gz" -C /var/www

    install -m 0644 "$BACKUP_DIR/routegate-manager.service" /etc/systemd/system/routegate-manager.service
    install -m 0644 "$BACKUP_DIR/routegate-agent.service" /etc/systemd/system/routegate-agent.service
    install -m 0600 "$BACKUP_DIR/manager.env" /etc/routegate/manager.env

    if [[ -s "$BACKUP_DIR/sing-box-config.json" ]]; then
      install -m 0600 "$BACKUP_DIR/sing-box-config.json" /etc/sing-box/config.json
    fi

    systemctl daemon-reload
    systemctl restart sing-box >/dev/null 2>&1 || true
    systemctl start routegate-manager >/dev/null 2>&1 || true
    systemctl start routegate-agent >/dev/null 2>&1 || true
    log "Rollback attempt completed; backup retained at $BACKUP_DIR"
  fi

  cleanup
  exit "$rc"
}

trap rollback ERR
trap cleanup EXIT

[[ $EUID -eq 0 ]] || { printf 'Must run as root.\n' >&2; exit 1; }
for command_name in curl pg_dump pg_restore psql sha256sum tar systemctl; do
  command -v "$command_name" >/dev/null || { printf 'Missing command: %s\n' "$command_name" >&2; exit 1; }
done
[[ -r /etc/routegate/manager.env ]] || { printf 'Missing /etc/routegate/manager.env\n' >&2; exit 1; }
[[ -r "$VALIDATION_SCRIPT" ]] || { printf 'Validation script is not readable.\n' >&2; exit 1; }

STAGE=preflight
for service in routegate-manager routegate-agent sing-box; do
  state=$(systemctl is-active "$service" || true)
  log "preflight ${service}=${state}"
  [[ "$state" == active ]]
done

actual_bundle_sha=$(sha256sum "$BUNDLE_FILE" | awk '{print $1}')
[[ "$actual_bundle_sha" == "$EXPECTED_BUNDLE_SHA" ]] || { printf 'Bundle SHA-256 mismatch.\n' >&2; exit 1; }
tar -xzf "$BUNDLE_FILE" -C "$WORK_DIR"
# shellcheck disable=SC1091
source "$WORK_DIR/metadata/manifest.env"
[[ "$COMMIT" == "$EXPECTED_COMMIT" ]]
[[ "$OS" == linux ]]
[[ "$ARCH" == amd64 ]]

EXPECTED_SCHEMA=$(
  find "$WORK_DIR/manager/migrations" -maxdepth 1 -type f -name '*.up.sql' -printf '%f\n' \
    | LC_ALL=C sort \
    | tail -n 1 \
    | sed 's/\.up\.sql$//'
)
[[ -n "$EXPECTED_SCHEMA" ]] || { printf 'Release bundle contains no database migrations.\n' >&2; exit 1; }
log "bundle expected schema=${EXPECTED_SCHEMA}"

set -a
# shellcheck disable=SC1091
source /etc/routegate/manager.env
set +a
DB_URL=${ROUTEGATE_DATABASE_URL:?ROUTEGATE_DATABASE_URL is required}

STAGE=backup
BACKUP_DIR="/root/routegate-backups/rg113i-${EXPECTED_COMMIT}-$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 0700 /root/routegate-backups "$BACKUP_DIR"
cp -a /usr/local/bin/routegate-manager "$BACKUP_DIR/routegate-manager"
cp -a /usr/local/bin/routegate-agent "$BACKUP_DIR/routegate-agent"
cp -a /etc/systemd/system/routegate-manager.service "$BACKUP_DIR/routegate-manager.service"
cp -a /etc/systemd/system/routegate-agent.service "$BACKUP_DIR/routegate-agent.service"
cp -a /etc/routegate/manager.env "$BACKUP_DIR/manager.env"
tar -czf "$BACKUP_DIR/manager-migrations.tar.gz" -C /opt routegate-manager/migrations
tar -czf "$BACKUP_DIR/frontend.tar.gz" -C /var/www routegate
if [[ -r /etc/sing-box/config.json ]]; then
  cp -a "$BACKUP_DIR/sing-box-config.json" "$BACKUP_DIR/sing-box-config.json"
fi
pg_dump --format=custom --no-owner --file="$BACKUP_DIR/routegate.pgdump" "$DB_URL"
chmod -R go-rwx "$BACKUP_DIR"
log "Backup complete: $BACKUP_DIR"

STAGE=deploy_files
systemctl stop routegate-agent
systemctl stop routegate-manager
MUTATED=1
install -m 0755 "$WORK_DIR/bin/routegate-manager" /usr/local/bin/routegate-manager
install -m 0755 "$WORK_DIR/bin/routegate-agent" /usr/local/bin/routegate-agent

rm -rf /opt/routegate-manager/migrations
cp -a "$WORK_DIR/manager/migrations" /opt/routegate-manager/migrations
chown -R routegate:routegate /opt/routegate-manager

find /var/www/routegate -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -a "$WORK_DIR/frontend/." /var/www/routegate/
chown -R root:root /var/www/routegate
find /var/www/routegate -type d -exec chmod 0755 {} +
find /var/www/routegate -type f -exec chmod 0644 {} +

install -m 0644 "$WORK_DIR/systemd/routegate-manager.service" /etc/systemd/system/routegate-manager.service
install -m 0644 "$WORK_DIR/systemd/routegate-agent.service" /etc/systemd/system/routegate-agent.service
systemctl daemon-reload

if grep -q '^ROUTEGATE_PUBLIC_URL=' /etc/routegate/manager.env; then
  sed -i "s#^ROUTEGATE_PUBLIC_URL=.*#ROUTEGATE_PUBLIC_URL=\"${PUBLIC_URL}\"#" /etc/routegate/manager.env
else
  printf 'ROUTEGATE_PUBLIC_URL="%s"\n' "$PUBLIC_URL" >> /etc/routegate/manager.env
fi
chmod 0600 /etc/routegate/manager.env

STAGE=manager_start
DB_MAY_BE_MUTATED=1
systemctl start routegate-manager
manager_ready=0
for _ in $(seq 1 45); do
  if curl -fsS http://127.0.0.1:8080/api/admin/health >/dev/null 2>&1; then
    manager_ready=1
    break
  fi
  sleep 1
done
[[ "$manager_ready" == 1 ]]

STAGE=schema_validation
latest_schema=$(psql "$DB_URL" -qAtc "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
if [[ "$latest_schema" != "$EXPECTED_SCHEMA" ]]; then
  printf 'Database schema mismatch after deploy: applied=%s expected=%s\n' "$latest_schema" "$EXPECTED_SCHEMA" >&2
  exit 1
fi
log "database schema=${latest_schema}"

STAGE=agent_start
systemctl start routegate-agent
for _ in $(seq 1 30); do
  systemctl is-active --quiet routegate-agent && break
  sleep 1
done
systemctl is-active --quiet routegate-agent

STAGE=observability_validation
chmod 0700 "$VALIDATION_SCRIPT"
"$VALIDATION_SCRIPT" "$EXPECTED_COMMIT"

STAGE=final_health
systemctl is-active --quiet routegate-manager
systemctl is-active --quiet routegate-agent
systemctl is-active --quiet sing-box
/usr/bin/sing-box check -c /etc/sing-box/config.json >/dev/null
public_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
[[ "$public_status" == 200 ]]

STAGE=complete
trap - ERR
log "production-like deploy and validation PASSED"
log "deployed_commit=$EXPECTED_COMMIT"
log "backup=$BACKUP_DIR"