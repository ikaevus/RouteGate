#!/usr/bin/env bash
# Failure-injection tests for the pinned schema158->159 Manager canary.
# Stub host only: no SSH, live PostgreSQL, systemd or GitHub operations.
# shellcheck disable=SC2016
set -Eeuo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
runner="$repo/scripts/production-like-rg140-canary.sh"
core="$repo/scripts/routegate-update-core.sh"
role="$repo/scripts/routegate-update-role.sh"
pre_sql="$repo/docs/operations/rg140-canary/preflight-schema-158.sql"
post_sql="$repo/docs/operations/rg140-canary/postflight-schema-159.sql"
workflow="$repo/.github/workflows/production-like-ops.yml"
deploy_workflow="$repo/.github/workflows/production-like-deploy.yml"
commit=d2a0990e91703a4b6e8744cab172b0d092953110
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

pass() { printf 'ok   %s\n' "$*"; }
fail() { printf 'FAIL %s\n' "$*" >&2; failures=$((failures + 1)); }
check() { local name=$1; shift; if "$@"; then pass "$name"; else fail "$name"; fi; }

[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "run as root: sudo bash $0" >&2; exit 1; }

# The statements the runner extracts from the pinned SQL files (same rule).
blocks="$work/blocks"
mkdir -p "$blocks"
for spec in "$pre_sql:P2" "$pre_sql:P3" "$post_sql:Q0" "$post_sql:Q3" "$post_sql:Q4" "$post_sql:Q5" "$post_sql:Q7"; do
  awk -v id="${spec##*:}" '
    index($0, "\\echo '"'"'== ") == 1 { if (inside) exit; inside = (index($0, "\\echo '"'"'== " id ".") == 1); next }
    inside && substr($0, 1, 1) != "\\" && $0 !~ /^ROLLBACK;/ { print }
  ' "${spec%:*}" >"$blocks/${spec##*:}.sql"
  [[ -s "$blocks/${spec##*:}.sql" ]] || { echo "check ${spec##*:} not found" >&2; exit 1; }
done

# --- stubs ---------------------------------------------------------------
stubs="$work/bin"
mkdir -p "$stubs"
cat >"$stubs/psql" <<'EOF'
#!/usr/bin/env bash
# Stub psql backed by $S/history (applied migrations, newest first).
S=$STUB_STATE
printf 'PGOPTIONS=%s ARGS=%s\n' "${PGOPTIONS:-}" "${*:2}" >>"$S/psql.log"
sql="" file=""
args=("$@")
for ((i = 1; i < ${#args[@]}; i++)); do
  case "${args[$i]}" in
    -v|-P|-F) i=$((i + 1)) ;;
    -f) i=$((i + 1)); file=${args[$i]} ;;
    -c) i=$((i + 1)); sql=${args[$i]} ;;
    -*c) i=$((i + 1)); sql=${args[$i]} ;;
  esac
done
if [[ -n "$file" ]]; then
  if grep -q '== P0\.' "$file"; then sections="P0 P1 P2 P3 P4 P5 P6 P7"; kind=pre; else sections="Q0 Q1 Q2 Q3 Q4 Q5 Q6"; kind=post; fi
  for s in $sections; do
    echo "== ${s}. section"
    echo " us-node | 11111111-2222-4333-8444-555555555555 | row"
  done
  if [[ "${STUB_FILE_FAIL:-}" == "$kind" ]]; then
    echo 'psql: error: connection to server at "db.internal" (10.1.2.3), port 5432 failed: FATAL:  password authentication failed for user "rgadmin"' >&2
    exit 2
  fi
  exit 0
fi
if [[ "$sql" == 'WITH withheld AS ('* ]]; then
  phase=before
  [[ "$(cat "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager")" != manager-new ]] || phase=after
  check=P3
  [[ "$sql" != *'not_deployed_by_active_version'* ]] || check=Q5
  awk -F'|' 'FILENAME == ARGV[1] { if (NF > 1) withheld[$2] = 1; next } !($2 in withheld)' \
    <(cat "$S/answer.$check" 2>/dev/null || true) "$S/served.$phase"
  exit 0
fi
for id in P2 P3 Q0 Q3 Q4 Q5 Q7; do
  if [[ "$sql" == "$(cat "$STUB_BLOCKS/$id.sql")" ]]; then
    if [[ "$id" == P3 && "$(cat "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager")" == manager-new ]]; then id=Q5; fi
    if [[ -f "$S/answer.$id" ]]; then cat "$S/answer.$id"; fi
    [[ "$id" != Q7 ]] || echo "0|0"
    [[ "$id" != Q0 ]] || [[ -f "$S/answer.Q0" ]] || printf '%s|%s\n' "$(head -n1 "$S/history")" "$(grep -c '^000159_' "$S/history")"
    exit 0
  fi
done
case "$sql" in
  *"SELECT md5(jsonb_build_object("*)
    if [[ "${STUB_IDENTITY_CHANGE:-0}" == 1 && "$(cat "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager")" == manager-new ]]; then echo changed; else echo original; fi ;;
  *"version > '"*)
    v=${sql#*version > \'}; v=${v%%\'*}
    awk -v v="$v" '$0 > v' "$S/history" | wc -l ;;
  *"ORDER BY applied_at DESC, version DESC LIMIT 1"*|*"ORDER BY version DESC LIMIT 1"*) head -n1 "$S/history" ;;
  *"ORDER BY applied_at DESC, version DESC"*) cat "$S/history" ;;
  *config_apply_jobs*) echo 0 ;;
  *"GROUP BY s.name"*) cat "$S/baseline" ;;
  *) echo "unexpected query: $sql" >&2; exit 3 ;;
esac
EOF
cat >"$stubs/pg_dump" <<'EOF'
#!/usr/bin/env bash
for arg in "$@"; do
  case "$arg" in --file=*) cp "$STUB_STATE/history" "${arg#--file=}"; exit 0 ;; esac
done
exit 2
EOF
cat >"$stubs/pg_restore" <<'EOF'
#!/usr/bin/env bash
echo "pg_restore $*" | sed 's#--dbname=[^ ]*#--dbname=<db>#' >>"$STUB_STATE/restore.log"
[[ "${STUB_RESTORE_FAIL:-0}" != 1 ]] || exit 1
cp "${*: -1}" "$STUB_STATE/history"
EOF
cat >"$stubs/systemctl" <<'EOF'
#!/usr/bin/env bash
# Stub systemctl: records every call; "starting" the Manager runs the stubbed
# migrations of the installed binary.
S=$STUB_STATE
echo "$*" >>"$S/systemctl.log"
root=$RG_UPDATE_ROOT
case "$1" in
  is-active) echo active ;;
  show) unit=${*: -1}; if [[ -f "$S/unit.$unit" ]]; then cat "$S/unit.$unit"; else printf 'loaded\nactive\nrunning\n100\n5\n0\n'; fi ;;
  stop) [[ "$2" != routegate-manager ]] || rm -f "$S/running" ;;
  start)
    [[ "$2" == routegate-manager ]] || exit 0
    if [[ "$(cat "$root/usr/local/bin/routegate-manager")" == manager-new ]]; then
      [[ "${STUB_AGENT_CHANGE:-0}" != 1 ]] || printf 'loaded\nactive\nrunning\n200\n9\n1\n' >"$S/unit.routegate-agent"
      for v in 000159_staged_account_transfers; do
        grep -qx "$v" "$S/history" || { printf '%s\n' "$v" | cat - "$S/history" >"$S/h" && mv "$S/h" "$S/history"; }
        if [[ "${STUB_MIGRATE_FAIL:-0}" == 1 ]]; then exit 1; fi
      done
    fi
    touch "$S/running" ;;
esac
exit 0
EOF
cat >"$stubs/curl" <<'EOF'
#!/usr/bin/env bash
S=$STUB_STATE
url=${*: -1}
if [[ "$url" == */api/admin/health ]]; then
  [[ -f "$S/running" ]] || exit 7
  exit 0
fi
printf '%s' "${STUB_PUBLIC_CODE:-200}"
EOF
cat >"$stubs/chown" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$stubs"/*

down_runner="$work/down-runner.sh"
cat >"$down_runner" <<'EOF'
#!/usr/bin/env bash
# Stub of deploy-production-like-bundle.sh --run-down-migration.
[[ "$1" == --run-down-migration && $# -eq 4 ]] || exit 2
echo "down $4" >>"$STUB_STATE/down.log"
[[ "${STUB_DOWN_FAIL:-0}" != 1 ]] || exit 1
[[ "$(head -n1 "$STUB_STATE/history")" == "$4" ]] || exit 1
sed -i 1d "$STUB_STATE/history"
EOF
chmod +x "$down_runner"

make_bundle() { # name manifest-commit latest-migration
  local stage="$work/stage-$1" tool
  rm -rf "$stage"
  mkdir -p "$stage"/{bin,frontend/assets,manager/migrations,systemd,metadata,tools}
  printf 'manager-new\n' >"$stage/bin/routegate-manager"
  printf 'agent-new\n' >"$stage/bin/routegate-agent"
  printf 'frontend-new\n' >"$stage/frontend/index.html"
  printf 'asset-new\n' >"$stage/frontend/assets/app.js"
  for v in 000158_explicit_account_protocol_preferences 000156_config_version_client_settings \
    000157_applied_version_account_protocols 000159_staged_account_transfers; do
    [[ "$v" > "$3" ]] && break
    printf -- '-- up\n' >"$stage/manager/migrations/$v.up.sql"
    printf -- '-- down\n' >"$stage/manager/migrations/$v.down.sql"
  done
  printf 'manager-unit-new\n' >"$stage/systemd/routegate-manager.service"
  printf 'agent-unit-new\n' >"$stage/systemd/routegate-agent.service"
  for tool in release_manifest.py routegate-update-core.sh routegate-update-role.sh routegate-update-transaction.sh routegate-update-verified.sh routegate-update-dispatch.py; do
    printf '# tool\n' >"$stage/tools/$tool"
  done
  printf 'FORMAT_VERSION=1\nVERSION=production-like\nCOMMIT=%s\nBUILD_DATE=2026-10-01T00:00:00Z\nOS=linux\nARCH=amd64\n' "$2" >"$stage/metadata/manifest.env"
  tar -czf "$work/$1.tar.gz" -C "$stage" .
}
make_bundle bundle "$commit" 000159_staged_account_transfers
make_bundle bundle157 "$commit" 000158_explicit_account_protocol_preferences
make_bundle othercommit fedcba9876543210fedcba9876543210fedcba98 000159_staged_account_transfers

# Fake host: hybrid layout, VPN configs, a published bootstrap tree, schema 155.
vpn_files=(usr/local/bin/routegate-agent etc/systemd/system/routegate-agent.service etc/routegate/agent.yaml
  etc/sing-box/config.json etc/wireguard/routegate-wg0.conf etc/hysteria/config.json
  etc/routegate-mtproto/config.toml etc/nginx/sites-available/routegate var/www/routegate/bootstrap/oldcommit/install-agent.sh)
setup_host() { # schema-history...
  root="$work/root" S="$work/state"
  rm -rf "$root" "$S"
  mkdir -p "$S" "$root"/{usr/local/bin,opt/routegate-manager/migrations,var/www/routegate/assets,etc/systemd/system,etc/routegate,root}
  printf 'manager-old\n' >"$root/usr/local/bin/routegate-manager"
  printf -- '-- up\n' >"$root/opt/routegate-manager/migrations/000158_explicit_account_protocol_preferences.up.sql"
  printf -- '-- down\n' >"$root/opt/routegate-manager/migrations/000158_explicit_account_protocol_preferences.down.sql"
  printf 'frontend-old\n' >"$root/var/www/routegate/index.html"
  printf 'asset-old\n' >"$root/var/www/routegate/assets/old.js"
  printf 'manager-unit-old\n' >"$root/etc/systemd/system/routegate-manager.service"
  printf 'ROUTEGATE_DATABASE_URL="postgres://rgadmin:S3cretPW@db.internal:5432/routegate"\nOTHER_SECRET=do-not-print\n' >"$root/etc/routegate/manager.env"
  local f
  for f in "${vpn_files[@]}"; do mkdir -p "$(dirname "$root/$f")"; printf 'original %s\n' "$f" >"$root/$f"; done
  printf '%s\n' "$@" >"$S/history"
  printf 'ru-node|1\nus-node|3\n' >"$S/baseline"
  printf 'ru-id|ru-account\nus-id|us-account-1\nus-id|us-account-2\nus-id|us-account-3\n' >"$S/served.before"
  cp "$S/served.before" "$S/served.after"
  printf 'ru-node|ready|1\nus-node|ready|3\n' >"$S/answer.Q4"
  printf 'ru-node|served|1\nus-node|served|3\n' >"$S/answer.P2"
  : >"$S/systemctl.log"; : >"$S/down.log"; : >"$S/restore.log"; : >"$S/psql.log"
  touch "$S/running"
  (cd "$root" && sha256sum "${vpn_files[@]}") >"$work/vpn.sums"
}
H155=(000158_explicit_account_protocol_preferences 000154_agent_maintenance_operations)
H158=(000159_staged_account_transfers "${H155[@]}")

run_update() { # bundle [sha] [preflight-sql] -> rc, out
  local bundle=$1 sha=${2:-} pre=${3:-$pre_sql}
  [[ -n "$sha" ]] || sha=$(sha256sum "$bundle" | awk '{print $1}')
  mkdir -p "$work/tmp"
  set +e
  out=$(PATH="$stubs:$PATH" RG_UPDATE_ROOT="$root" STUB_STATE="$S" STUB_BLOCKS="$blocks" TMPDIR="$work/tmp" \
    ROUTEGATE_UPDATE_HEALTH_POLL_SECONDS=0 ROUTEGATE_UPDATE_DRAIN_POLL_SECONDS=0 \
    bash "$runner" "$commit" "$bundle" "$sha" "$core" "$role" "$down_runner" "$pre" "$post_sql" 2>&1)
  rc=$?
  set -e
}
leaks='S3cretPW|do-not-print|postgres://|db\.internal|rgadmin'
vpn_unchanged() { (cd "$root" && sha256sum --quiet -c "$work/vpn.sums") >/dev/null 2>&1; }
only_manager_touched() { ! grep -Ev '^(is-active|show|daemon-reload)( |$)|^(start|stop) routegate-manager$' "$S/systemctl.log" | grep -q .; }
history_head() { head -n1 "$S/history"; }
no_new_migrations_installed() { ! compgen -G "$root/opt/routegate-manager/migrations/000159_*" >/dev/null; }

# --- runner: success from 155 --------------------------------------------
setup_host "${H155[@]}"
run_update "$work/bundle.tar.gz"
check "runner: 158 -> 159 succeeds" bash -c '[[ $1 -eq 0 ]] && grep -q "RESULT=updated commit=" <<<"$2"' _ "$rc" "$out"
check "runner: schema is 159 after the update" test "$(history_head)" == 000159_staged_account_transfers
check "runner: new Manager binary, migrations and frontend installed" bash -c '
  [[ $(cat "$1/usr/local/bin/routegate-manager") == manager-new && -f "$1/opt/routegate-manager/migrations/000159_staged_account_transfers.up.sql"
     && $(cat "$1/var/www/routegate/index.html") == frontend-new && ! -e "$1/var/www/routegate/assets/old.js" ]]' _ "$root"
check "runner: Agent, VPN, nginx files and bootstrap artifacts unchanged" vpn_unchanged
check "runner: only the Manager service is stopped/started" only_manager_touched
check "runner: Agent/VPN invariance reported" grep -q "Agent, VPN runtimes, nginx, maintenance dispatch and bootstrap artifacts: unchanged" <<<"$out"
check "runner: full P0-P7 and Q0-Q6 output kept" bash -c '[[ $(grep -c "^== P[0-7]\." <<<"$1") -eq 8 && $(grep -c "^== Q[0-6]\." <<<"$1") -eq 7 ]]' _ "$out"
check "runner: exact commit verified through the binary checksum" grep -q "installed Manager binary matches bundle commit=$commit" <<<"$out"
check "runner: warns that bootstrap artifacts for the commit are unpublished" grep -q "bootstrap artifacts for this commit are not published" <<<"$out"
backup=$(find "$root/root/routegate-backups" -mindepth 1 -maxdepth 1 -type d)
check "runner: backup named for maintenance dispatch and complete" bash -c '
  [[ $(basename "$1") =~ ^update-management-[0-9a-f]{40}-[0-9]{8}T[0-9]{6}Z$ ]] || exit 1
  for f in routegate-manager routegate-manager.service manager.env manager-migrations.tar.gz frontend.tar.gz routegate.pgdump role.meta manager-update.meta; do [[ -s "$1/$f" ]] || exit 1; done
  grep -qx "DATABASE_SCHEMA=000158_explicit_account_protocol_preferences" "$1/manager-update.meta"
  grep -qx "TARGET_COMMIT=$2" "$1/manager-update.meta"' _ "$backup" "$commit"
check "runner: Manager stopped before the database backup" bash -c 'grep -n "" "$1" | grep -m1 "stop routegate-manager" >/dev/null' _ "$S/systemctl.log"
check "runner: read-only session for the checks" grep -q 'default_transaction_read_only=on' "$S/psql.log"
check "runner: no rollback on success" bash -c '[[ ! -s "$1" && ! -s "$2" ]]' _ "$S/down.log" "$S/restore.log"
check "runner: prints no URL or secret" bash -c '! grep -Eq "$2" <<<"$1"' _ "$out" "$leaks"
check "runner: removes its working directory" bash -c '[[ -z $(ls -A "$1") ]]' _ "$work/tmp"

# --- runner: failures roll back ------------------------------------------
rolled_back() { # expected down migrations
  [[ $rc -eq 1 ]] && grep -q "ROLLBACK COMPLETE" <<<"$out" && grep -q "RESULT=failed" <<<"$out" \
    && [[ "$(history_head)" == 000158_explicit_account_protocol_preferences ]] \
    && [[ "$(cat "$root/usr/local/bin/routegate-manager")" == manager-old ]] \
    && [[ "$(cat "$root/var/www/routegate/index.html")" == frontend-old ]] \
    && no_new_migrations_installed && [[ -f "$S/running" ]] \
    && [[ "$(awk '{print $2}' "$S/down.log" | tr '\n' ' ')" == "$1" ]] \
    && grep -q -- '--clean --if-exists --no-owner --no-privileges --exit-on-error' "$S/restore.log" \
    && vpn_unchanged && only_manager_touched
}
setup_host "${H155[@]}"
STUB_MIGRATE_FAIL=1 run_update "$work/bundle.tar.gz"
check "runner: Manager start/migration failure rolls back 159 and restores 158" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
printf 'us-node|us-account-3|vless|{vless}|awaiting_apply|{vless}\n' >"$S/answer.Q5"
run_update "$work/bundle.tar.gz"
check "runner: postflight account refusal rolls back 159" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
printf 'us-node|v7|applied|t\n' >"$S/answer.Q3"
run_update "$work/bundle.tar.gz"
check "runner: active version without snapshot rolls back" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
printf 'fi-node|v1|pending|f\n' >"$S/answer.Q3"
run_update "$work/bundle.tar.gz"
check "runner: pending inactive version without snapshot does not block" test "$rc" -eq 0

setup_host "${H155[@]}"
printf 'ru-node|ready|1\nus-node|ready|2\n' >"$S/answer.Q4"
sed -i '/us-account-3/d' "$S/served.after"
run_update "$work/bundle.tar.gz"
check "runner: fewer served accounts than before rolls back" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
STUB_FILE_FAIL=post run_update "$work/bundle.tar.gz"
check "runner: postflight psql error rolls back" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
STUB_PUBLIC_CODE=502 run_update "$work/bundle.tar.gz"
check "runner: public frontend failure rolls back" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
STUB_MIGRATE_FAIL=1 STUB_DOWN_FAIL=1 run_update "$work/bundle.tar.gz"
check "runner: failed rollback is reported as incomplete" bash -c '[[ $1 -eq 1 ]] && grep -q "ROLLBACK INCOMPLETE" <<<"$2" && ! grep -q "ROLLBACK COMPLETE" <<<"$2"' _ "$rc" "$out"

# --- runner: Agent/VPN change is detected, not rolled back ---------------
setup_host "${H155[@]}"
STUB_AGENT_CHANGE=1 run_update "$work/bundle.tar.gz"
check "runner: Agent state change gives exit 6 with labels only" bash -c '
  [[ $1 -eq 6 ]] && grep -q "state changed during the update: unit:routegate-agent" <<<"$2" && ! grep -q "running 200" <<<"$2"' _ "$rc" "$out"

# --- runner: refusals before any change ----------------------------------
unchanged_host() {
  [[ "$(cat "$root/usr/local/bin/routegate-manager")" == manager-old ]] && ! grep -q '^stop' "$S/systemctl.log" \
    && [[ ! -e "$root/root/routegate-backups" ]] && [[ "$(history_head)" == "$1" ]]
}
setup_host "${H155[@]}"
printf 'us-node|aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee|vless|{vless}|awaiting_apply|{vless}\n' >"$S/answer.P3"
run_update "$work/bundle.tar.gz"
check "runner: preflight predicting refused accounts is refused (exit 3)" bash -c '[[ $1 -eq 3 ]]' _ "$rc"
check "runner: refused preflight changes nothing" unchanged_host 000158_explicit_account_protocol_preferences

setup_host "${H155[@]}"
printf 'fi-node|unchecked_no_snapshot|1\nus-node|served|3\n' >"$S/answer.P2"
run_update "$work/bundle.tar.gz"
check "runner: unchecked node in the preflight is refused" bash -c '[[ $1 -eq 3 ]]' _ "$rc"

setup_host 000154_agent_maintenance_operations
run_update "$work/bundle.tar.gz"
check "runner: unexpected schema refused, nothing changed" bash -c '[[ $1 -eq 3 ]]' _ "$rc"
check "runner: unexpected schema leaves host as is" unchanged_host 000154_agent_maintenance_operations

setup_host "${H155[@]}"
run_update "$work/bundle157.tar.gz"
check "runner: bundle not targeting 159 refused" bash -c '[[ $1 -eq 3 ]]' _ "$rc"
check "runner: wrong-target bundle leaves host as is" unchanged_host 000158_explicit_account_protocol_preferences

setup_host "${H155[@]}"
run_update "$work/othercommit.tar.gz"
check "runner: bundle of another commit fails before any change" bash -c '[[ $1 -ne 0 ]] && grep -q "bundle commit does not match" <<<"$2"' _ "$rc" "$out"
check "runner: other-commit bundle leaves host as is" unchanged_host 000158_explicit_account_protocol_preferences

setup_host "${H155[@]}"
run_update "$work/bundle.tar.gz" "$(printf '0%.0s' {1..64})"
check "runner: bundle checksum mismatch fails before any change" bash -c '[[ $1 -ne 0 ]]' _ "$rc"
check "runner: checksum mismatch leaves host as is" unchanged_host 000158_explicit_account_protocol_preferences

setup_host "${H155[@]}"
cp "$pre_sql" "$work/altered.sql"; printf -- '-- extra\n' >>"$work/altered.sql"
run_update "$work/bundle.tar.gz" "" "$work/altered.sql"
check "runner: altered preflight SQL refused (exit 2)" bash -c '[[ $1 -eq 2 ]]' _ "$rc"
set +e; bash "$runner" "$commit" 2>/dev/null; rc=$?; set -e
check "runner: wrong argument count refused (exit 2)" test "$rc" -eq 2
set +e; bash "$runner" "ABC" "$work/bundle.tar.gz" x "$core" "$role" "$down_runner" "$pre_sql" "$post_sql" 2>/dev/null; rc=$?; set -e
check "runner: invalid commit refused (exit 2)" test "$rc" -eq 2


setup_host 000159_staged_account_transfers 000158_explicit_account_protocol_preferences
run_update "$work/bundle.tar.gz"
check "runner: repeat deployment on schema159 refused before changes" bash -c '[[ $1 -eq 3 ]]' _ "$rc"
check "runner: repeat refusal leaves host as is" unchanged_host 000159_staged_account_transfers

setup_host "${H155[@]}"
printf '1|1\n' >"$S/answer.Q7"
run_update "$work/bundle.tar.gz"
check "runner: unexpected transfer reservation rolls back" rolled_back "000159_staged_account_transfers "

setup_host "${H155[@]}"
STUB_IDENTITY_CHANGE=1 run_update "$work/bundle.tar.gz"
check "runner: token/device/account identity changes roll back" rolled_back "000159_staged_account_transfers "

if ((failures)); then printf "%d checks failed\n" "$failures" >&2; exit 1; fi
echo "all RG140 canary runner checks passed"
