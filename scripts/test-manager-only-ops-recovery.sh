#!/usr/bin/env bash
# Isolated Manager-only recovery contract: no real SSH or systemctl.
set -Eeuo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
ops="$repo/scripts/production-like-ops.sh"
wf="$repo/.github/workflows/production-like-ops.yml"
python3 - "$wf" "$ops" <<'PY'
import pathlib, sys
w, s = (pathlib.Path(p).read_text() for p in sys.argv[1:])
assert "'operation=start-manager-only') operation=start-manager-only ;;" in w
assert "validate-deploy-rollback-cycle|restart-control-plane|start-manager-only|restart-sing-box" in w
assert '"$operation" == "start-manager-only"' in w
assert "GITHUB_REF" in w and "refs/heads/main" in w
assert "sudo flock -w 600 /run/lock/routegate-production-like.lock" in w
assert 'systemctl start "$unit"' in s
assert "manager-start=passed active=true local-health=200" in s
assert "    start-manager-only)\n      start_manager_only\n      ;;" in s
print("fixed allowlist and main-only guard: PASS")
PY
[[ $(id -u) -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cat > "$tmp/systemctl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_CALLS"
case "$*" in
  "show --property=LoadState --value routegate-manager.service") echo loaded ;;
  "is-active --quiet routegate-manager.service") [[ -e "$FAKE_ACTIVE" ]] ;;
  "start routegate-manager.service") touch "$FAKE_ACTIVE" ;;
  *) exit 86 ;;
esac
SH
cat > "$tmp/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *"http://127.0.0.1:8080/api/admin/health"* ]] || exit 87
printf 200
SH
chmod 700 "$tmp/systemctl" "$tmp/curl"
export PATH="$tmp:$PATH" FAKE_ACTIVE="$tmp/active" FAKE_CALLS="$tmp/calls"
: > "$FAKE_CALLS"
bash "$ops" start-manager-only >"$tmp/first"
grep -Fq 'manager-start=passed active=true local-health=200' "$tmp/first"
[[ -e "$FAKE_ACTIVE" ]]
[[ $(grep -Fxc 'start routegate-manager.service' "$FAKE_CALLS") == 1 ]]
! grep -Eiq 'restart|stop|agent|nginx|sing-box|hysteria|mtproto|wireguard' "$FAKE_CALLS"
echo "manager-only startup: PASS"
: > "$FAKE_CALLS"
bash "$ops" start-manager-only >"$tmp/again"
grep -Fq 'manager-start=already-active' "$tmp/again"
! grep -Fxq 'start routegate-manager.service' "$FAKE_CALLS"
echo "already-active idempotency: PASS"
: > "$FAKE_CALLS"
if bash "$ops" start-manager-only-other >"$tmp/rejected" 2>&1; then
  echo "unlisted operation accepted" >&2; exit 1
fi
[[ ! -s "$FAKE_CALLS" ]]
echo "unknown operation rejected: PASS"
