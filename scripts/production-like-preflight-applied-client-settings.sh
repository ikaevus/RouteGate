#!/usr/bin/env bash
# Read-only preflight of the applied-client-settings update (PR #499) on the
# production-like Manager database, run by the Production-like Ops workflow.
#
#   production-like-preflight-applied-client-settings.sh <preflight-schema-155.sql>
#
# It runs exactly one pinned SQL file (verified by SHA-256) and nothing else:
#   1. requires root, psql and a readable Manager environment file;
#   2. checks read-only that the schema is 000155 with no 000156+ migration,
#      and stops without touching the database otherwise;
#   3. runs the SQL with psql -X, ON_ERROR_STOP, no pager, and a session that
#      is read-only by default (the SQL also runs in its own READ ONLY
#      transaction ending with ROLLBACK), with statement and lock timeouts.
# It never prints the database URL, the environment file or credentials, and
# never installs anything or changes privileges. A successful run only means
# the checks completed: their results may still block the update.
#
# Exit codes: 0 checks completed; 2 usage or SQL checksum mismatch;
# 3 unexpected schema; 4 psql, environment or connection unavailable;
# 5 the checks failed while running (psql exit status in the log).
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

# preflight-schema-155.sql from commit b72637cf122d4d3db0c988ebb436e87bbe00d6a9
readonly EXPECTED_SQL_SHA256=324095e8671a2cb1bcf2f30e41b9e1658e27718a1817d32df5e1e7bf41182a49
readonly EXPECTED_SCHEMA_PREFIX=000155_
readonly MANAGER_ENV=${ROUTEGATE_PREFLIGHT_MANAGER_ENV:-/etc/routegate/manager.env}
readonly PSQL_OPTIONS='-c default_transaction_read_only=on -c statement_timeout=120s -c lock_timeout=5s -c idle_in_transaction_session_timeout=60s -c application_name=routegate-ops-preflight'

log() { printf '[routegate-preflight] %s\n' "$*"; }
fail() { local code=$1; shift; printf '[routegate-preflight] ERROR: %s\n' "$*" >&2; exit "$code"; }

# Connection errors can name the host, port, user or database; never pass them on.
redact() {
  sed -E \
    -e 's#postgres(ql)?://[^[:space:]]*#<redacted-url>#g' \
    -e 's#(connection to server|could not connect|password authentication|no pg_hba|FATAL:).*#\1 <details redacted>#'
}

sql_file=${1:-}
[[ $# -eq 1 && -n "$sql_file" && -f "$sql_file" && -r "$sql_file" ]] || fail 2 "usage: $0 <preflight-schema-155.sql>"
[[ ${EUID:-$(id -u)} -eq 0 ]] || fail 4 "must run as root to read the Manager environment file"
command -v sha256sum >/dev/null 2>&1 || fail 4 "sha256sum is not available on this host"
actual_sha=$(sha256sum "$sql_file" | awk '{print $1}')
[[ "$actual_sha" == "$EXPECTED_SQL_SHA256" ]] || fail 2 "SQL file does not match the pinned preflight (sha256 ${actual_sha})"
log "sql=preflight-schema-155.sql sha256=${actual_sha} source-commit=b72637cf122d4d3db0c988ebb436e87bbe00d6a9"

command -v psql >/dev/null 2>&1 || fail 4 "psql is not installed on this host; nothing was installed"
[[ -f "$MANAGER_ENV" ]] || fail 4 "Manager environment file is missing"
[[ -r "$MANAGER_ENV" ]] || fail 4 "Manager environment file is not readable; privileges were not changed"

# Load only the database URL, in this process, without echoing anything.
database_url=$(
  set +x
  set -a
  # shellcheck disable=SC1090
  source "$MANAGER_ENV" >/dev/null 2>&1 || exit 1
  printf '%s' "${ROUTEGATE_DATABASE_URL:-}"
) || fail 4 "Manager environment file could not be loaded"
[[ -n "$database_url" ]] || fail 4 "Manager environment file does not define the database connection"

run_psql() { # extra psql arguments...
  PGOPTIONS="$PSQL_OPTIONS" PGCONNECT_TIMEOUT=10 psql "$database_url" -X -q -v ON_ERROR_STOP=1 -P pager=off "$@"
}

schema_err=$(mktemp)
trap 'rm -f "$schema_err"' EXIT
if ! schema=$(run_psql -At -F '|' -c "
    SELECT (SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1),
           (SELECT count(*) FROM schema_migrations WHERE version >= '000156'),
           current_setting('transaction_read_only')" 2>"$schema_err"); then
  printf '[routegate-preflight] detail: %s\n' "$(redact <"$schema_err" | head -n 3 | tr '\n' ' ')" >&2
  fail 4 "cannot read the schema version (connection or permission problem); nothing was changed"
fi
IFS='|' read -r latest newer read_only <<<"$schema"
log "schema latest=${latest:-none} migrations_156_plus=${newer:-?} session_read_only=${read_only:-?}"
[[ "$read_only" == "on" ]] || fail 4 "the session is not read-only; refusing to run the checks"
if [[ "$latest" != "$EXPECTED_SCHEMA_PREFIX"* || "$newer" != "0" ]]; then
  fail 3 "expected schema 000155 with no 000156+ migration; the database was not adapted and no migration was run"
fi

log "checks starting (P0-P7)"
set +e
run_psql -f "$sql_file" 2> >(redact >&2)
rc=$?
set -e
wait
if ((rc != 0)); then
  fail 5 "the checks stopped with psql exit status ${rc}; see the output above"
fi
log "checks completed: review P0-P7; completion does not approve the update"
