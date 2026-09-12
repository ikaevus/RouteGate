#!/usr/bin/env bash
#
# Regression test for scripts/deploy-production-like-bundle.sh's
# rollback_database_to_backup(): a failed pg_restore must never destroy live
# data on its way to reporting failure.
#
# Background: rollback_database_to_backup down-migrates the live schema to
# match a backup's recorded schema version, then runs
# `pg_restore --clean --if-exists` to restore the backup over it. If a down
# migration ever leaves behind so much as one column/constraint it should
# have removed (a realistic authoring mistake - this test injects exactly
# that), pg_restore's --clean step can hit a dependency it doesn't know how
# to drop and abort partway through. Without --single-transaction on both the
# down-migration psql invocations and the pg_restore invocation itself, that
# partial failure can already have deleted live rows before it aborts, while
# still reporting a (correct) non-zero exit code - the database is left
# silently worse off than either its pre-rollback or its target state. This
# test reproduces that exact scenario and asserts the fix holds: the failure
# is still reported loudly, but live data present before the rollback attempt
# must survive completely intact.
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT="$ROOT_DIR/scripts/deploy-production-like-bundle.sh"
MIGRATIONS_DIR="$ROOT_DIR/backend/migrations"

fail() {
  printf 'test-production-like-rollback: %s\n' "$*" >&2
  exit 1
}

# --- Always-on static guard (needs no database): the two flags this test
# exists to protect must actually be present in the script, so a future edit
# that quietly drops one is caught even where no test database is available.
rollback_fn=$(sed -n '/^rollback_database_to_backup() {/,/^}/p' "$SCRIPT")
# shellcheck disable=SC2016 # intentionally grepping literal shell source containing "$down_file"
down_file_line=$(printf '%s\n' "$rollback_fn" | grep -F -- '-f "$down_file"')
[[ -n "$down_file_line" ]] || fail "could not find the down-migration psql invocation to check"
printf '%s\n' "$down_file_line" | grep -Fq -- '--single-transaction' \
  || fail "down-migration psql invocation in rollback_database_to_backup is missing --single-transaction"
printf '%s\n' "$rollback_fn" | grep -A5 -F -- 'pg_restore' | grep -Fq -- '--single-transaction' \
  || fail "pg_restore invocation in rollback_database_to_backup is missing --single-transaction"
printf 'test-production-like-rollback: static --single-transaction guard OK\n'

# --- Full empirical reproduction: only runs when a real Postgres is
# reachable, matching the ROUTEGATE_TEST_DATABASE_URL convention already used
# by backend/internal/db's integration tests.
DB_URL=${ROUTEGATE_TEST_DATABASE_URL:-}
if [[ -z "$DB_URL" ]]; then
  printf 'test-production-like-rollback: ROUTEGATE_TEST_DATABASE_URL not set, skipping live-database reproduction\n'
  exit 0
fi
for tool in psql pg_dump pg_restore; do
  command -v "$tool" >/dev/null 2>&1 || {
    printf 'test-production-like-rollback: %s not available, skipping live-database reproduction\n' "$tool"
    exit 0
  }
done

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

apply_migrations() {
  local db_url=$1 dir=$2 stop_at=${3:-} f version already
  psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "
    CREATE TABLE IF NOT EXISTS schema_migrations (
      version TEXT PRIMARY KEY,
      applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );" >/dev/null
  for f in "$dir"/*.up.sql; do
    version=$(basename "$f" .up.sql)
    already=$(psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = '$version')")
    [[ "$already" == "t" ]] && continue
    psql "$db_url" -v ON_ERROR_STOP=1 -f "$f" >/dev/null 2>&1
    psql "$db_url" -v ON_ERROR_STOP=1 -qAtc "INSERT INTO schema_migrations (version) VALUES ('$version');" >/dev/null
    if [[ -n "$stop_at" && "$version" == "$stop_at" ]]; then
      break
    fi
  done
  return 0
}

# Fresh throwaway database (a distinct name inside the same server addressed
# by ROUTEGATE_TEST_DATABASE_URL) so this test never touches the CI backend
# suite's own database or data.
ADMIN_DB_URL=$(printf '%s' "$DB_URL" | sed -E 's#/[^/?]+(\?|$)#/postgres\1#')
TEST_DB_NAME="rg_rollback_regression_$$"
TEST_DB_URL=$(printf '%s' "$DB_URL" | sed -E "s#/[^/?]+(\?|\$)#/${TEST_DB_NAME}\1#")
psql "$ADMIN_DB_URL" -v ON_ERROR_STOP=1 -qAtc "DROP DATABASE IF EXISTS ${TEST_DB_NAME};" >/dev/null
psql "$ADMIN_DB_URL" -v ON_ERROR_STOP=1 -qAtc "CREATE DATABASE ${TEST_DB_NAME};" >/dev/null
trap 'psql "$ADMIN_DB_URL" -qAtc "DROP DATABASE IF EXISTS ${TEST_DB_NAME};" >/dev/null 2>&1 || true; rm -rf "$TMP_DIR"' EXIT

apply_migrations "$TEST_DB_URL" "$MIGRATIONS_DIR" 000148_vpn_account_telegram_contact

psql "$TEST_DB_URL" -v ON_ERROR_STOP=1 -qAtc "
  INSERT INTO vpn_accounts (username, protocol, display_name, status)
  VALUES ('rollback-regression-account', 'sing-box', 'Rollback Regression', 'active');
  INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
  SELECT id, 'rollback-regression-legacy-token', 'active' FROM vpn_accounts WHERE username='rollback-regression-account';
" >/dev/null

BACKUP_DIR="$TMP_DIR/backup"
mkdir -p "$BACKUP_DIR"
pg_dump --format=custom --no-owner --file="$BACKUP_DIR/routegate.pgdump" "$TEST_DB_URL"
BACKUP_SCHEMA=$(psql "$TEST_DB_URL" -qAtc "SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1")
printf 'FORMAT_VERSION=1\nDATABASE_SCHEMA=%s\n' "$BACKUP_SCHEMA" >"$BACKUP_DIR/backup.meta"

apply_migrations "$TEST_DB_URL" "$MIGRATIONS_DIR"

# A row that exists ONLY in the live database (not in the backup just taken),
# so we can tell a genuine atomic-rollback ("live data survives") apart from
# a lucky coincidence where the backup already contained the same data.
psql "$TEST_DB_URL" -v ON_ERROR_STOP=1 -qAtc "
  INSERT INTO vpn_accounts (username, protocol, display_name, status)
  VALUES ('rollback-regression-live-marker', 'sing-box', 'Live Marker', 'active');
  INSERT INTO vpn_subscription_tokens (vpn_account_id, token_hash, status)
  SELECT id, 'rollback-regression-live-marker-token', 'active' FROM vpn_accounts WHERE username='rollback-regression-live-marker';
" >/dev/null

# Inject the realistic authoring mistake: a down migration whose second
# statement targets the wrong column name, so it silently no-ops (IF EXISTS)
# instead of actually reverting 000151's schema change - leaving deliveries
# still carrying a subscription_token_id column/FK the backup's dump predates.
BROKEN_MIGRATIONS_DIR="$TMP_DIR/migrations"
cp -a "$MIGRATIONS_DIR" "$BROKEN_MIGRATIONS_DIR"
cat >"$BROKEN_MIGRATIONS_DIR/000151_delivery_token_generation.down.sql" <<'SQL'
DROP INDEX IF EXISTS idx_deliveries_subscription_token_id;
ALTER TABLE deliveries DROP COLUMN IF EXISTS subscription_token_id_typo_that_should_have_failed_review;
SQL

rg_update_path() { printf '%s' "$BROKEN_MIGRATIONS_DIR"; }
log() { :; }
# shellcheck disable=SC2317
sed -n '/^rollback_database_to_backup() {/,/^}/p' "$SCRIPT" >"$TMP_DIR/extracted.sh"
# shellcheck source=/dev/null
source "$TMP_DIR/extracted.sh"

set +e
rollback_database_to_backup "$BACKUP_DIR" "$TEST_DB_URL" >"$TMP_DIR/rollback.log" 2>&1
rollback_rc=$?
set -e

[[ "$rollback_rc" -ne 0 ]] || fail "rollback with a broken down migration must fail, got success"

live_marker=$(psql "$TEST_DB_URL" -qAtc "SELECT token_hash FROM vpn_subscription_tokens WHERE token_hash = 'rollback-regression-live-marker-token';")
[[ "$live_marker" == "rollback-regression-live-marker-token" ]] || {
  cat "$TMP_DIR/rollback.log" >&2
  fail "live data present before the failed rollback attempt did not survive - pg_restore destroyed data on its way to failing"
}

printf 'test-production-like-rollback: live-database reproduction OK (rollback failed loudly, live data intact)\n'
