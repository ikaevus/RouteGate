#!/usr/bin/env bash
# Tests for the preflight-applied-client-settings-155 ops operation, with
# stubs only (no PostgreSQL, SSH or GitHub access):
#   * the host runner: pinned SQL checksum, schema gate, read-only psql
#     options, redacted connection errors, full output, exit codes;
#   * the workflow: exact request mapping and allow-list (other operations
#     unchanged), main-only dispatch, remote copy/run/cleanup, full output kept
#     for the artifact, sanitized log and #268 comment, exit propagation.
# Run as root (the runner requires it): sudo bash scripts/test-production-like-preflight.sh
#
# Checks pass their values to `bash -c '...' _ "$value"` as positional
# parameters, so the single-quoted scripts must not expand.
# shellcheck disable=SC2016
set -Eeuo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
runner="$repo/scripts/production-like-preflight-applied-client-settings.sh"
sql="$repo/docs/operations/applied-client-settings/preflight-schema-155.sql"
workflow="$repo/.github/workflows/production-like-ops.yml"
op=preflight-applied-client-settings-155
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0

pass() { printf 'ok   %s\n' "$*"; }
fail() { printf 'FAIL %s\n' "$*" >&2; failures=$((failures + 1)); }
check() { local name=$1; shift; if "$@"; then pass "$name"; else fail "$name"; fi; }

[[ ${EUID:-$(id -u)} -eq 0 ]] || { echo "run as root: sudo bash $0" >&2; exit 1; }

# --- stubs ---------------------------------------------------------------
stubs="$work/bin"
mkdir -p "$stubs"
cat >"$stubs/psql" <<'EOF'
#!/usr/bin/env bash
# Stub psql: records its invocation and answers per STUB_MODE.
{ printf 'PGOPTIONS=%s\n' "${PGOPTIONS:-}"; printf 'ARGS=%s\n' "$*"; } >>"$STUB_LOG"
if [[ " $* " == *" -c "* ]]; then
  case "$STUB_MODE" in
    ok|midfail) echo '000155_config_apply_trigger_invariant_repair|0|on' ;;
    schema158) echo '000158_explicit_account_protocol_preferences|3|on' ;;
    schema154) echo '000154_something|0|on' ;;
    writable) echo '000155_config_apply_trigger_invariant_repair|0|off' ;;
    connfail)
      echo 'psql: error: connection to server at "db.example.internal" (10.9.8.7), port 5432 failed: FATAL:  password authentication failed for user "rgadmin"' >&2
      echo 'hint: postgresql://rgadmin:S3cretPW@db.example.internal:5432/routegate' >&2
      exit 2 ;;
  esac
  exit 0
fi
echo "FILE_RUN" >>"$STUB_LOG"
for section in P0 P1 P2 P3 P4 P5 P6 P7; do
  echo "== ${section}. section"
  for i in $(seq 1 20); do echo " node-$i | 00000000-0000-4000-8000-00000000000$((i % 10)) | row"; done
  if [[ "$STUB_MODE" == midfail && "$section" == P3 ]]; then
    echo 'psql:preflight-schema-155.sql:120: ERROR:  relation "vpn_account_protocols" does not exist' >&2
    exit 3
  fi
done
echo ROLLBACK
EOF
cat >"$stubs/sudo" <<'EOF'
#!/usr/bin/env bash
exec "$@"
EOF
cat >"$stubs/flock" <<'EOF'
#!/usr/bin/env bash
# flock -w SECONDS LOCKFILE command...
shift 3
exec "$@"
EOF
cat >"$stubs/scp" <<'EOF'
#!/usr/bin/env bash
# Copies to the "remote" path; records remote paths.
src=${*: -2:1}
dst=${*: -1}
dst=${dst#*:}
[[ "${STUB_SCP_FAIL:-}" == "$(basename "$src")" ]] && exit 1
cp "$src" "$dst"
echo "$dst" >>"$STUB_REMOTE_FILES"
EOF
cat >"$stubs/ssh" <<'EOF'
#!/usr/bin/env bash
# Runs the remote command locally.
exec bash -c "${*: -1}"
EOF
cat >"$stubs/gh" <<'EOF'
#!/usr/bin/env bash
cat >>"$STUB_GH_LOG"
EOF
chmod +x "$stubs"/*

env_file="$work/manager.env"
printf 'ROUTEGATE_DATABASE_URL=postgresql://rgadmin:S3cretPW@db.example.internal:5432/routegate\nOTHER_SECRET=do-not-print\n' >"$env_file"
chmod 600 "$env_file"
leaks='S3cretPW|do-not-print|postgresql://|db\.example\.internal|rgadmin'

run_runner() { # mode sql-file [extra args...] -> sets rc, out
  local mode=$1; shift
  : >"$work/stub.log"
  set +e
  out=$(PATH="$stubs:$PATH" STUB_MODE=$mode STUB_LOG="$work/stub.log" TMPDIR="$work/tmp" \
    ROUTEGATE_PREFLIGHT_MANAGER_ENV="${RUNNER_ENV:-$env_file}" bash "$runner" "$@" 2>&1)
  rc=$?
  set -e
}
mkdir -p "$work/tmp"

# --- runner ----------------------------------------------------------------
run_runner ok "$sql"
check "runner: completes with exit 0" test "$rc" -eq 0
check "runner: keeps the full P0-P7 output" bash -c '[[ $(grep -c "^== P[0-7]\." <<<"$1") -eq 8 && $(wc -l <<<"$1") -gt 160 ]]' _ "$out"
check "runner: reports the pinned source and schema" bash -c 'grep -q "source-commit=b72637cf" <<<"$1" && grep -q "session_read_only=on" <<<"$1"' _ "$out"
check "runner: psql -X, ON_ERROR_STOP, no pager" grep -q -- '-X -q -v ON_ERROR_STOP=1 -P pager=off' "$work/stub.log"
check "runner: read-only session with timeouts" bash -c 'grep -q "default_transaction_read_only=on" "$1" && grep -q "statement_timeout=" "$1" && grep -q "lock_timeout=" "$1"' _ "$work/stub.log"
check "runner: prints no URL or secret" bash -c '! grep -Eq "$2" <<<"$1"' _ "$out" "$leaks"
check "runner: removes its temporary files" bash -c '[[ -z $(ls -A "$1") ]]' _ "$work/tmp"

for mode in schema158 schema154; do
  run_runner "$mode" "$sql"
  check "runner: $mode refused with exit 3" test "$rc" -eq 3
  check "runner: $mode never runs the checks" bash -c '! grep -q FILE_RUN "$1"' _ "$work/stub.log"
done
run_runner writable "$sql"
check "runner: non read-only session refused" bash -c '[[ $1 -eq 4 ]] && ! grep -q FILE_RUN "$2"' _ "$rc" "$work/stub.log"

run_runner connfail "$sql"
check "runner: connection failure exits 4" test "$rc" -eq 4
check "runner: connection failure redacts URL, host, user and password" bash -c '! grep -Eq "$2" <<<"$1"' _ "$out" "$leaks"

run_runner midfail "$sql"
check "runner: psql failure while checking exits 5" test "$rc" -eq 5
check "runner: output before the failure is kept" bash -c 'grep -q "^== P2\." <<<"$1" && grep -q "does not exist" <<<"$1"' _ "$out"

cp "$sql" "$work/altered.sql"; printf -- '-- extra\nSELECT 1;\n' >>"$work/altered.sql"
run_runner ok "$work/altered.sql"
check "runner: altered SQL refused with exit 2" bash -c '[[ $1 -eq 2 ]] && [[ ! -s "$2" ]]' _ "$rc" "$work/stub.log"
run_runner ok "$sql" extra-argument
check "runner: extra arguments refused" test "$rc" -eq 2

nopsql="$work/nopsql"; mkdir -p "$nopsql"
for tool in bash sha256sum awk sed mktemp rm head tr cat id; do ln -sf "$(command -v "$tool")" "$nopsql/$tool"; done
set +e
out=$(PATH="$nopsql" ROUTEGATE_PREFLIGHT_MANAGER_ENV="$env_file" "$nopsql/bash" "$runner" "$sql" 2>&1); rc=$?
set -e
check "runner: missing psql exits 4 without installing" bash -c '[[ $1 -eq 4 ]] && grep -q "psql is not installed" <<<"$2"' _ "$rc" "$out"
RUNNER_ENV="$work/missing.env" run_runner ok "$sql"
check "runner: missing environment file exits 4" test "$rc" -eq 4
printf 'OTHER=1\n' >"$work/empty.env"
RUNNER_ENV="$work/empty.env" run_runner ok "$sql"
check "runner: environment without the database URL exits 4" test "$rc" -eq 4

# --- workflow ----------------------------------------------------------------
step() { # workflow step name -> its run script
  python3 - "$workflow" "$1" <<'EOF'
import sys, yaml
workflow, name = sys.argv[1], sys.argv[2]
for step in yaml.safe_load(open(workflow))["jobs"]["operate"]["steps"]:
    if step.get("name") == name:
        sys.stdout.write(step["run"])
        sys.exit(0)
sys.exit(f"step not found: {name}")
EOF
}
step "Resolve allow-listed operation" >"$work/resolve.sh"

resolve() { # event body-or-op ref -> prints operation or "exit N"
  local event=$1 value=$2 ref=${3:-refs/heads/main} output="$work/resolve.out"
  : >"$output"
  set +e
  if [[ "$event" == workflow_dispatch ]]; then
    EVENT_NAME=$event DISPATCH_OPERATION=$value REQUEST_BODY='' GITHUB_REF=$ref GITHUB_OUTPUT=$output bash "$work/resolve.sh" >/dev/null 2>&1
  else
    EVENT_NAME=$event DISPATCH_OPERATION='' REQUEST_BODY=$value GITHUB_REF=$ref GITHUB_OUTPUT=$output bash "$work/resolve.sh" >/dev/null 2>&1
  fi
  local code=$?
  set -e
  if ((code == 0)); then sed -n 's/^operation=//p' "$output"; else echo "exit $code"; fi
}

check "workflow: exact issue request accepted" test "$(resolve issues "operation=$op")" == "$op"
check "workflow: main dispatch accepted" test "$(resolve workflow_dispatch "$op")" == "$op"
check "workflow: dispatch from another ref refused" test "$(resolve workflow_dispatch "$op" refs/heads/feature)" == "exit 2"
for body in "operation=$op " "operation=$op"$'\n'"sql=SELECT 1" "operation=$op&ref=feature" "operation=${op}x" "operation=preflight" "sql=SELECT 1" "operation=diagnose;$op"; do
  check "workflow: request '${body//$'\n'/\\n}' refused" test "$(resolve issues "$body")" == "exit 2"
done
check "workflow: arbitrary dispatch operation refused" test "$(resolve workflow_dispatch "run-sql")" == "exit 2"

# The other operations resolve exactly as before, from main or another ref.
git -C "$repo" show origin/main:.github/workflows/production-like-ops.yml >"$work/main-workflow.yml" 2>/dev/null \
  || git -C "$repo" show main:.github/workflows/production-like-ops.yml >"$work/main-workflow.yml"
python3 - "$work/main-workflow.yml" >"$work/resolve-main.sh" <<'EOF'
import sys, yaml
for step in yaml.safe_load(open(sys.argv[1]))["jobs"]["operate"]["steps"]:
    if step.get("name") == "Resolve allow-listed operation":
        sys.stdout.write(step["run"])
EOF
for old in diagnose diagnose-sing-box diagnose-agent-task validate validate-deploy-rollback-cycle restart-control-plane restart-sing-box restart-wireguard restart-hysteria2 restart-mtproto reconcile-mtproto-port-conflict renew-certificate; do
  new_issue=$(resolve issues "operation=$old")
  new_dispatch=$(resolve workflow_dispatch "$old" refs/heads/feature)
  cp "$work/resolve.sh" "$work/resolve-new.sh"; cp "$work/resolve-main.sh" "$work/resolve.sh"
  old_issue=$(resolve issues "operation=$old"); old_dispatch=$(resolve workflow_dispatch "$old" refs/heads/feature)
  cp "$work/resolve-new.sh" "$work/resolve.sh"
  check "workflow: $old unchanged" test "$new_issue|$new_dispatch" == "$old_issue|$old_dispatch"
done

step "Run allow-listed remote operation" >"$work/remote.sh"
remote() { # mode [scp-fail] -> rc, log, output file
  local mode=$1 scp_fail=${2:-}
  export GITHUB_RUN_ID=$RANDOM$RANDOM RUNNER_TEMP="$work/runner" GITHUB_OUTPUT="$work/gh-output"
  rm -rf "$RUNNER_TEMP"; mkdir -p "$RUNNER_TEMP"; : >"$GITHUB_OUTPUT"; : >"$work/remote-files"; : >"$work/stub.log"
  set +e
  log=$(cd "$repo" && PATH="$stubs:$PATH" HOME="$work/home" STUB_MODE=$mode STUB_LOG="$work/stub.log" \
    STUB_REMOTE_FILES="$work/remote-files" STUB_SCP_FAIL=$scp_fail ROUTEGATE_PREFLIGHT_MANAGER_ENV="$env_file" \
    ROUTEGATE_HOST=manager.example ROUTEGATE_USER=ops OPERATION=$op TARGET_SHA='' BUNDLE_FILE='' BUNDLE_SHA='' \
    bash "$work/remote.sh" 2>&1)
  step_rc=$?
  set -e
  remote_rc=$(sed -n 's/^rc=//p' "$GITHUB_OUTPUT")
}
remote_files_removed() { local f; while read -r f; do [[ ! -e "$f" ]] || return 1; done <"$work/remote-files"; }

remote ok
check "workflow: remote run succeeds with rc=0" bash -c '[[ $1 -eq 0 && $2 == 0 ]]' _ "$step_rc" "$remote_rc"
check "workflow: copies exactly the runner and the pinned SQL" bash -c '[[ $(wc -l <"$1") -eq 2 ]] && grep -q "routegate-preflight-.*\.sh$" "$1" && grep -q "routegate-preflight-.*\.sql$" "$1"' _ "$work/remote-files"
check "workflow: removes the remote files" remote_files_removed
check "workflow: keeps the full output for the artifact" bash -c '[[ $(grep -c "^== P[0-7]\." "$1") -eq 8 ]]' _ "$RUNNER_TEMP/routegate-ops-output.txt"
check "workflow: log shows only status lines" bash -c '! grep -q "^== P" <<<"$1" && ! grep -q "0000-4000" <<<"$1" && grep -q "checks completed" <<<"$1"' _ "$log"

remote schema158
check "workflow: schema mismatch propagates rc=3" test "$remote_rc" == 3
check "workflow: schema mismatch still removes remote files" remote_files_removed
remote midfail
check "workflow: psql failure propagates rc=5" test "$remote_rc" == 5
remote ok production-like-preflight-applied-client-settings.sh
check "workflow: copy failure gives rc=4 and cleans up" bash -c '[[ $1 == 4 ]] && [[ ! -s "$2" || $(wc -l <"$2") -eq 0 ]]' _ "$remote_rc" "$work/remote-files"

step "Publish sanitized audit result" >"$work/audit.sh"
audit() { # rc
  : >"$work/gh.log"
  (cd "$repo" && PATH="$stubs:$PATH" STUB_GH_LOG="$work/gh.log" GH_TOKEN=x OPERATION=$op REMOTE_RC=$1 \
    EVENT_NAME=workflow_dispatch GITHUB_REPOSITORY=owner/repo GITHUB_RUN_ID=42 RUNNER_TEMP="$work/runner" bash "$work/audit.sh")
}
remote ok; audit 0
check "workflow: #268 comment has status, artifact and approval caveat only" bash -c 'grep -q "PASSED" "$1" && grep -q "routegate-preflight-applied-client-settings-155-42" "$1" && grep -q "does not approve\|still stop the update" "$1" && ! grep -q "0000-4000" "$1" && ! grep -q "== P" "$1"' _ "$work/gh.log"
remote schema158; audit 3
check "workflow: failed preflight comment says FAILED" grep -q FAILED "$work/gh.log"

step "Propagate remote failure" >"$work/propagate.sh"
check "workflow: nonzero rc fails the job" bash -c '! REMOTE_RC=3 bash "$1"' _ "$work/propagate.sh"
check "workflow: zero rc passes the job" bash -c 'REMOTE_RC=0 bash "$1"' _ "$work/propagate.sh"

check "workflow: artifact kept 7 days for this operation only" python3 - "$workflow" <<'EOF'
import sys, yaml
steps = yaml.safe_load(open(sys.argv[1]))["jobs"]["operate"]["steps"]
upload = [s for s in steps if s.get("name") == "Upload full preflight result"]
checkout = [s for s in steps if s.get("name") == "Checkout trusted main"]
ok = (len(upload) == 1 and upload[0]["with"]["retention-days"] == 7
      and "preflight-applied-client-settings-155" in upload[0]["if"]
      and checkout and checkout[0]["with"]["ref"] == "main")
sys.exit(0 if ok else 1)
EOF

if ((failures)); then
  printf '%d check(s) failed\n' "$failures" >&2
  exit 1
fi
echo "all preflight ops checks passed"
