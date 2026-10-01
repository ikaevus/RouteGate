#!/usr/bin/env bash
# Local rehearsal of scripts/production-like-update-manager.sh on a disposable
# PostgreSQL with the real Manager builds: the base (schema 000155) and the
# target (PR #499, schema 000158). Never run it against a real host or a real
# database. It needs root (the runner requires it), Go, git, curl, python3 and
# the PostgreSQL client tools, plus a PostgreSQL superuser connection used only
# to create and drop databases whose names end in "_rehearsal".
#
#   REHEARSAL_ADMIN_URL='postgres://postgres@/postgres?host=/var/tmp/rg-pg&port=5433' \
#     sudo -E docs/operations/applied-client-settings/rehearse-manager-only-update.sh
#
# Host side: a fake root (RG_UPDATE_ROOT) with the base Manager installed and
# Agent/VPN/nginx/bootstrap files; a stub systemctl that records every call and
# really starts/stops the installed Manager binary; real pg_dump/pg_restore, the
# real down-migration runner of deploy-production-like-bundle.sh and real curl.
# The bundle is assembled from the target build (binary, migrations, units).
#
# Scenarios (expected exit code of the runner):
#   refused-edge-cases  3  the PR #499 rehearsal seed: withheld/unchecked nodes
#   success             0  clean data (every active account served): 155 -> 158
#   migration-failure   1  000158 fails inside Manager start -> rollback to 155
#   late-failure        1  public frontend check fails after a full upgrade -> rollback
# For each it reports schema, Manager health, installed binary, Agent/VPN file
# checksums, systemctl calls on other units, and whether schema and data equal
# the state before the run (built-in roles.updated_at is re-stamped by every
# Manager start and is ignored).
set -Eeuo pipefail

: "${REHEARSAL_ADMIN_URL:?set REHEARSAL_ADMIN_URL to a disposable PostgreSQL superuser connection}"
BASE_REF=${BASE_REF:-802d4b75b415bf324563654eadebba32442cabd6}
TARGET_REF=${TARGET_REF:-b72637cf122d4d3db0c988ebb436e87bbe00d6a9}
HTTP_PORT=${REHEARSAL_HTTP_PORT:-18080}
WEB_PORT=${REHEARSAL_WEB_PORT:-18081}
DB=manager_update_rehearsal

[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "run as root" >&2; exit 2; }
repo=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)
W=$(mktemp -d)
chmod 700 "$W"
admin() { psql "$REHEARSAL_ADMIN_URL" -X -q -v ON_ERROR_STOP=1 "$@"; }
db_url() { # database name -> URL on the same server as the admin connection
  python3 - "$REHEARSAL_ADMIN_URL" "$1" <<'EOF'
import sys, urllib.parse as u
p = u.urlsplit(sys.argv[1])
print(u.urlunsplit((p.scheme, p.netloc, "/" + sys.argv[2], p.query, p.fragment)))
EOF
}
URL=$(db_url "$DB")
web=""
# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() {
  [[ -z "$web" ]] || kill "$web" 2>/dev/null || true
  [[ ! -f "$W/manager.pid" ]] || kill "$(cat "$W/manager.pid")" 2>/dev/null || true
  git -C "$repo" worktree remove --force "$W/base" >/dev/null 2>&1 || true
  git -C "$repo" worktree remove --force "$W/target" >/dev/null 2>&1 || true
  admin -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "DROP DATABASE IF EXISTS seed155_rehearsal" \
    -c "DROP DATABASE IF EXISTS clean155_rehearsal" >/dev/null 2>&1 || true
  rm -rf "$W"
}
trap cleanup EXIT

echo "== builds: base $BASE_REF, target $TARGET_REF"
git -C "$repo" worktree add --quiet --detach "$W/base" "$BASE_REF"
git -C "$repo" worktree add --quiet --detach "$W/target" "$TARGET_REF"
(cd "$W/base/backend" && go build -o "$W/base-manager" ./cmd/routegate-manager)
(cd "$W/target/backend" && go build -o "$W/target-manager" ./cmd/routegate-manager)

echo "== databases: seed schema 155 with the base build, derive the clean variant"
admin -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB"
git -C "$repo" show "$TARGET_REF:docs/operations/applied-client-settings/rehearsal/seed_schema155_test.go.txt" \
  >"$W/base/backend/internal/db/zz_rehearsal_seed155_test.go"
# The seed refuses any database whose name does not end in _rehearsal.
(cd "$W/base/backend" && REHEARSAL_DATABASE_URL="$URL" go test ./internal/db -run '^TestRehearsalSeed155$' -count=1 >/dev/null)
admin -c "DROP DATABASE IF EXISTS seed155_rehearsal" -c "CREATE DATABASE seed155_rehearsal TEMPLATE $DB"
# Clean data, like the live preflight: suspend exactly the accounts the pinned
# preflight lists in P3, and the accounts of nodes it predicts as unchecked;
# detach active versions without a derivable snapshot (inactive ones remain).
p3=$(awk '
  index($0, "\\echo '"'"'== ") == 1 { if (inside) exit; inside = (index($0, "\\echo '"'"'== P3.") == 1); next }
  inside && substr($0, 1, 1) != "\\" && $0 !~ /^ROLLBACK;/ { print }
' "$repo/docs/operations/applied-client-settings/preflight-schema-155.sql")
withheld=$(psql "$URL" -X -q -At -F '|' -c "$p3" | awk -F'|' '{print $2}' | paste -sd, -)
[[ "$withheld" =~ ^[0-9a-f,-]+$ ]] || { echo "unexpected P3 result of the seed" >&2; exit 1; }
psql "$URL" -X -q -v ON_ERROR_STOP=1 -v ids="{$withheld}" <<'SQL'
BEGIN;
UPDATE vpn_accounts SET status = 'suspended'
WHERE status = 'active' AND (id = ANY (:'ids'::uuid[])
  OR server_id IN (SELECT id FROM servers WHERE name IN ('broken-reality', 'corrupt-ports', 'huge-port', 'shadowsocks-corrupt')));
UPDATE servers SET active_config_version_id = NULL
WHERE name IN ('broken-reality', 'corrupt-ports', 'huge-port', 'shadowsocks-corrupt');
COMMIT;
SQL
admin -c "DROP DATABASE IF EXISTS clean155_rehearsal" -c "CREATE DATABASE clean155_rehearsal TEMPLATE $DB"

mkdir -p "$W/stubs"
cat >"$W/stubs/systemctl" <<'EOF'
#!/usr/bin/env bash
# Records every call; really starts/stops the installed Manager binary.
echo "$*" >>"$REH/systemctl.log"
pid="$REH_PID"
alive() { [[ -f "$pid" ]] && kill -0 "$(cat "$pid")" 2>/dev/null; }
case "$1" in
  is-active) if [[ "$2" == routegate-manager ]] && ! alive; then echo inactive; exit 3; fi; echo active ;;
  show) printf 'loaded\nactive\nrunning\n100\n5\n0\n' ;;
  stop)
    if [[ "$2" == routegate-manager ]] && alive; then
      kill "$(cat "$pid")"
      for _ in $(seq 100); do alive || break; sleep 0.1; done
      rm -f "$pid"
    fi ;;
  start)
    [[ "$2" == routegate-manager ]] || exit 0
    alive && exit 0
    ( cd "$RG_UPDATE_ROOT/opt/routegate-manager" && set -a && source "$RG_UPDATE_ROOT/etc/routegate/manager.env" && set +a \
      && exec "$RG_UPDATE_ROOT/usr/local/bin/routegate-manager" ) >>"$REH/manager.log" 2>&1 &
    echo $! >"$pid" ;;
esac
exit 0
EOF
printf '#!/usr/bin/env bash\nexit 0\n' >"$W/stubs/chown"
chmod +x "$W/stubs"/*
export PATH="$W/stubs:$PATH" REH_PID="$W/manager.pid"
export RG_UPDATE_HEALTH_URL="http://127.0.0.1:${HTTP_PORT}/api/admin/health"

make_bundle() { # name [broken]
  local st="$W/stage-$1" t
  mkdir -p "$st"/{bin,frontend,manager,systemd,metadata,tools}
  cp "$W/target-manager" "$st/bin/routegate-manager"
  printf 'agent placeholder\n' >"$st/bin/routegate-agent"
  printf '<html>target frontend</html>\n' >"$st/frontend/index.html"
  cp -r "$W/target/backend/migrations" "$st/manager/migrations"
  [[ "${2:-}" != broken ]] || printf '\nSELECT 1/0;\n' >>"$st/manager/migrations/000158_explicit_account_protocol_preferences.up.sql"
  cp "$W/target/deploy/systemd/routegate-manager.service" "$st/systemd/" 2>/dev/null || printf 'unit\n' >"$st/systemd/routegate-manager.service"
  printf 'unit\n' >"$st/systemd/routegate-agent.service"
  for t in release_manifest.py routegate-update-core.sh routegate-update-role.sh routegate-update-transaction.sh routegate-update-verified.sh routegate-update-dispatch.py; do
    cp "$repo/scripts/$t" "$st/tools/$t" 2>/dev/null || printf '# placeholder\n' >"$st/tools/$t"
  done
  printf 'FORMAT_VERSION=1\nVERSION=production-like\nCOMMIT=%s\nBUILD_DATE=%s\nOS=linux\nARCH=amd64\n' \
    "$TARGET_REF" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$st/metadata/manifest.env"
  tar -czf "$W/$1.tar.gz" -C "$st" .
}
make_bundle good
make_bundle broken broken

vpn=(usr/local/bin/routegate-agent etc/systemd/system/routegate-agent.service etc/routegate/agent.yaml
  etc/sing-box/config.json etc/wireguard/routegate-wg0.conf etc/hysteria/config.json
  etc/routegate-mtproto/config.toml etc/nginx/sites-available/routegate var/www/routegate/bootstrap/base/install-agent.sh)
dump() { # output
  { pg_dump --schema-only --no-owner "$URL"; pg_dump --data-only --no-owner --exclude-table-data=public.roles "$URL"; } \
    | grep -Ev '^\\(un)?restrict ' >"$1"
}

scenario() { # name template bundle public-url expected-rc
  local name=$1 template=$2 bundle=$3 public=$4 want=$5 rc root f schema newer health binary files others same leak
  export REH="$W/run-$name" RG_UPDATE_ROOT="$W/run-$name/root"
  root=$RG_UPDATE_ROOT
  mkdir -p "$REH" "$root"/{usr/local/bin,opt/routegate-manager,var/www/routegate,etc/systemd/system,etc/routegate,root}
  admin -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB TEMPLATE $template"
  cp "$W/base-manager" "$root/usr/local/bin/routegate-manager"
  cp -r "$W/base/backend/migrations" "$root/opt/routegate-manager/migrations"
  printf '<html>base frontend</html>\n' >"$root/var/www/routegate/index.html"
  cp "$W/base/deploy/systemd/routegate-manager.service" "$root/etc/systemd/system/" 2>/dev/null \
    || printf 'unit\n' >"$root/etc/systemd/system/routegate-manager.service"
  printf 'ROUTEGATE_DATABASE_URL="%s"\nROUTEGATE_HTTP_ADDR=127.0.0.1:%s\nROUTEGATE_ENV=development\nROUTEGATE_MASTER_KEY_FILE=%s/master.key\nROUTEGATE_PUBLIC_URL=https://manager.example\n' \
    "$URL" "$HTTP_PORT" "$REH" >"$root/etc/routegate/manager.env"
  for f in "${vpn[@]}"; do mkdir -p "$(dirname "$root/$f")"; printf 'original %s\n' "$f" >"$root/$f"; done
  (cd "$root" && sha256sum "${vpn[@]}") >"$REH/vpn.sums"
  systemctl start routegate-manager
  for _ in $(seq 120); do curl -fsS "$RG_UPDATE_HEALTH_URL" >/dev/null 2>&1 && break; sleep 0.5; done
  curl -fsS "$RG_UPDATE_HEALTH_URL" >/dev/null
  : >"$REH/systemctl.log"
  dump "$REH/db.before"
  set +e
  ROUTEGATE_PUBLIC_URL_OVERRIDE=$public ROUTEGATE_UPDATE_HEALTH_POLL_SECONDS=1 TMPDIR="$REH" \
    bash "$repo/scripts/production-like-update-manager.sh" "$TARGET_REF" "$W/$bundle.tar.gz" \
    "$(sha256sum "$W/$bundle.tar.gz" | awk '{print $1}')" \
    "$repo/scripts/routegate-update-core.sh" "$repo/scripts/routegate-update-role.sh" "$repo/scripts/deploy-production-like-bundle.sh" \
    "$repo/docs/operations/applied-client-settings/preflight-schema-155.sql" \
    "$repo/docs/operations/applied-client-settings/postflight-schema-158.sql" >"$REH/output.txt" 2>&1
  rc=$?
  set -e
  schema=$(psql "$URL" -XqAtc "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
  newer=$(psql "$URL" -XqAtc "SELECT count(*) FROM schema_migrations WHERE version > '000155_config_apply_trigger_invariant_repair'")
  health=$(curl -fsS -o /dev/null -w '%{http_code}' "$RG_UPDATE_HEALTH_URL" 2>/dev/null || echo down)
  binary=other
  cmp -s "$root/usr/local/bin/routegate-manager" "$W/target-manager" && binary=target
  cmp -s "$root/usr/local/bin/routegate-manager" "$W/base-manager" && binary=base
  files=CHANGED
  (cd "$root" && sha256sum --quiet -c "$REH/vpn.sums") >/dev/null 2>&1 && files=unchanged
  others=$(grep -Ev '^(is-active|show|daemon-reload)( |$)|^(start|stop) routegate-manager$' "$REH/systemctl.log" | tr '\n' ';' || true)
  dump "$REH/db.after"
  same=no
  cmp -s "$REH/db.before" "$REH/db.after" && same=yes
  leak=$(grep -cF -e "$URL" -e 'postgres://' "$REH/output.txt" || true)
  printf '%-19s rc=%s (want %s) schema=%s newer_than_155=%s manager_health=%s binary=%s agent_vpn_files=%s other_unit_calls=[%s] db_equal_to_before=%s url_in_output=%s\n' \
    "$name" "$rc" "$want" "$schema" "$newer" "$health" "$binary" "$files" "$others" "$same" "$leak"
  grep -E 'RESULT=|ROLLBACK (COMPLETE|INCOMPLETE)|REFUSED|preflight gate|postflight gate|database rollback' "$REH/output.txt" \
    | sed -e 's/^/    /' -e 's#backup retained at .*#backup retained#'
  systemctl stop routegate-manager
  [[ "$rc" == "$want" && "$files" == unchanged && -z "$others" && "$leak" == 0 ]] || return 1
  case "$want" in
    0) [[ "$schema" == 000158_explicit_account_protocol_preferences && "$binary" == target && "$health" == 200 ]] ;;
    *) [[ "$schema" == 000155_config_apply_trigger_invariant_repair && "$newer" == 0 && "$binary" == base && "$health" == 200 && "$same" == yes ]] ;;
  esac
}

mkdir -p "$W/web"
printf 'ok\n' >"$W/web/index.html"
python3 -m http.server "$WEB_PORT" --bind 127.0.0.1 --directory "$W/web" >/dev/null 2>&1 &
web=$!
sleep 1
status=0
scenario refused-edge-cases seed155_rehearsal good "http://127.0.0.1:$WEB_PORT" 3 || status=1
scenario success clean155_rehearsal good "http://127.0.0.1:$WEB_PORT" 0 || status=1
scenario migration-failure clean155_rehearsal broken "http://127.0.0.1:$WEB_PORT" 1 || status=1
scenario late-failure clean155_rehearsal good "http://127.0.0.1:1" 1 || status=1
if ((status == 0)); then echo "== rehearsal PASSED"; else echo "== rehearsal FAILED" >&2; fi
exit "$status"
