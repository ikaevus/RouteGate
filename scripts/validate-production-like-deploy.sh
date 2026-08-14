#!/usr/bin/env bash
set -Eeuo pipefail

EXPECTED_COMMIT=${1:?expected commit is required}
PUBLIC_URL=${ROUTEGATE_PUBLIC_URL_OVERRIDE:-https://us.routegate.org}

log() {
  printf '[production-like-deploy] %s\n' "$*"
}

for service in routegate-manager routegate-agent sing-box; do
  systemctl is-active --quiet "$service"
  log "${service}=active"
done

/usr/bin/sing-box check -c /etc/sing-box/config.json >/dev/null

manager_status=$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/admin/health)
[[ "$manager_status" == 200 ]]

public_status=$(curl -sS -o /dev/null -w '%{http_code}' "$PUBLIC_URL/")
[[ "$public_status" == 200 ]]

log "deploy-only validation passed"
log "deployed_commit=$EXPECTED_COMMIT"
