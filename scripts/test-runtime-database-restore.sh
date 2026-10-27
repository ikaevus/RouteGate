#!/usr/bin/env bash
# CI-only rehearsal; never a host restore or existing-role repair procedure.
set -euo pipefail
[[ ${GITHUB_ACTIONS:-} == true && ${ROUTEGATE_E2E_ISOLATED:-} == 1 ]]
[[ ${ROUTEGATE_DATABASE_URL:-} == 'postgres://routegate:routegate_ci_password@127.0.0.1:5432/routegate_workspace_e2e?sslmode=disable' ]]
[[ ${RG140_POSTGRES_CONTAINER:-} =~ ^[a-f0-9]{64}$ ]]
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
sql() { psql "$ROUTEGATE_DATABASE_URL" -X -v ON_ERROR_STOP=1 -qAtc "$1"; }
ledger=$(sql 'SELECT string_agg(version, chr(10) ORDER BY version) FROM schema_migrations')
# Use the PostgreSQL service's own client version, not a host pg_dump version.
docker exec "$RG140_POSTGRES_CONTAINER" pg_dump -U routegate -d routegate_workspace_e2e --format=custom --no-owner > "$work/database.dump"
docker exec -i "$RG140_POSTGRES_CONTAINER" pg_restore -U routegate -d routegate_workspace_e2e --clean --if-exists --no-owner --no-privileges --exit-on-error < "$work/database.dump"
[[ $(sql 'SELECT string_agg(version, chr(10) ORDER BY version) FROM schema_migrations') == "$ledger" ]]
[[ $(sql "SELECT has_table_privilege('rg140_workspace_runtime','public.users','INSERT')") == f ]]
# Preserve ledger readability to distinguish ACL failure from version failure.
sql "GRANT USAGE ON SCHEMA public TO rg140_workspace_runtime; GRANT SELECT ON schema_migrations TO rg140_workspace_runtime; ALTER ROLE rg140_workspace_runtime LOGIN PASSWORD 'runtime_ci_only_password'"
rc=0
ROUTEGATE_DATABASE_URL='postgres://rg140_workspace_runtime:runtime_ci_only_password@127.0.0.1:5432/routegate_workspace_e2e?sslmode=disable' \
ROUTEGATE_DATABASE_MIGRATION_MODE=external \
timeout 15s "$RUNNER_TEMP/routegate-manager" > "$work/startup.log" 2>&1 || rc=$?
[[ $rc == 1 ]]
grep -q 'requires complete runtime privileges' "$work/startup.log"
[[ $(sql "SELECT has_table_privilege('rg140_workspace_runtime','public.users','INSERT')") == f ]]
# A fresh fixture role permits the unchanged browser suite to exercise the
# restored database. This is explicitly NOT reconciliation of an existing role.
sql 'DROP OWNED BY rg140_workspace_runtime; DROP ROLE rg140_workspace_runtime'
timeout 60s "$RUNNER_TEMP/routegate-manager" --create-runtime-role rg140_workspace_runtime
