#!/usr/bin/env bash
# Read-only fixed-scope diagnostic. No apply, cutover, cleanup or credential output.
set -euo pipefail
set +x
for svc in routegate-manager routegate-agent sing-box; do
  state=$(systemctl is-active "$svc" 2>/dev/null || true)
  printf '%s=%s\n' "$svc" "${state:-unknown}"
done
python3 - <<'PY'
import pathlib,re
p=pathlib.Path('/etc/routegate/agent.yaml')
if p.is_file():
    value=re.search(r'^\s*service_control_enabled\s*:\s*(true|false)\s*(?:#.*)?$',p.read_text(),re.M)
    print('agent_service_control='+ (value.group(1) if value else 'default_or_unrecognized'))
PY
set -a
source /etc/routegate/manager.env >/dev/null 2>&1
set +a
export PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s -c application_name=routegate-rg140-inspect'
export PGCONNECT_TIMEOUT=10
if ! psql "${ROUTEGATE_DATABASE_URL:?}" -X -q -v ON_ERROR_STOP=1 -P pager=off -f "$1" 2>/dev/null; then
  echo 'inspection_failed (database details suppressed)'
  exit 1
fi
