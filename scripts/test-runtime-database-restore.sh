#!/usr/bin/env bash
# CI-only rehearsal; never a host restore or existing-role repair procedure.
set -Eeuo pipefail
# Report location only: xtrace would expose the private identity snapshot.
trap 'echo "runtime restore fixture failed at line $LINENO" >&2' ERR
[[ ${GITHUB_ACTIONS:-} == true && ${ROUTEGATE_E2E_ISOLATED:-} == 1 ]]
[[ ${ROUTEGATE_DATABASE_URL:-} == 'postgres://routegate:routegate_ci_password@127.0.0.1:5432/routegate_workspace_e2e?sslmode=disable' ]]
[[ ${RG140_POSTGRES_CONTAINER:-} =~ ^[a-f0-9]{64}$ ]]
# The owner-only snapshot contains a password verifier: never log or upload it.
umask 077
identity_file="$RUNNER_TEMP/rg140-restored-role-identity"
sql() { psql "$ROUTEGATE_DATABASE_URL" -X -v ON_ERROR_STOP=1 -qAtc "$1"; }
identity() {
  sql "SELECT row_to_json(r)::text FROM pg_authid r WHERE rolname='rg140_workspace_runtime';
       SELECT row_to_json(s)::text FROM pg_db_role_setting s
       WHERE setrole=(SELECT oid FROM pg_roles WHERE rolname='rg140_workspace_runtime') ORDER BY setdatabase"
}
runtime_url='postgres://rg140_workspace_runtime:runtime_ci_only_password@127.0.0.1:5432/routegate_workspace_e2e?sslmode=disable'
verify_identity() {
  [[ -s $identity_file && $(identity) == $(cat "$identity_file") ]]
  [[ $(psql "$runtime_url" -X -v ON_ERROR_STOP=1 -qAtc 'SELECT current_user') == rg140_workspace_runtime ]]
  # A successful connection alone proves nothing if host authentication is trust.
  if psql 'postgres://rg140_workspace_runtime:wrong_ci_password@127.0.0.1:5432/routegate_workspace_e2e?sslmode=disable' \
      -X -v ON_ERROR_STOP=1 -qAtc 'SELECT 1' > /dev/null 2>&1; then
    echo 'runtime fixture accepted an incorrect password' >&2
    exit 1
  fi
}
if [[ ${1:-} == --verify-identity ]]; then
  verify_identity
  rm -- "$identity_file"
  echo 'restored runtime identity and authentication preserved after browser suite'
  exit 0
fi
[[ $# == 0 ]]
identity > "$identity_file"
[[ $(sql "SELECT rolcanlogin AND rolpassword IS NOT NULL FROM pg_authid WHERE rolname='rg140_workspace_runtime'") == t ]]
verify_identity
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
ledger=$(sql 'SELECT string_agg(version, chr(10) ORDER BY version) FROM schema_migrations')
# Use the PostgreSQL service's own client version, not a host pg_dump version.
docker exec "$RG140_POSTGRES_CONTAINER" pg_dump -U routegate -d routegate_workspace_e2e --format=custom --no-owner > "$work/database.dump"
# Only ACL TOC entries from the dump created immediately above are replayed.
# This is NOT an allowlist/security validator for arbitrary backup archives.
docker exec -i "$RG140_POSTGRES_CONTAINER" pg_restore --list < "$work/database.dump" > "$work/toc"
awk '$4 == "ACL" || ($4 == "DEFAULT" && $5 == "ACL")' "$work/toc" > "$work/acl.list"
[[ -s $work/acl.list ]]
# Record object ACLs by name, since restore changes relation OIDs.
acls() {
  sql "SELECT n.nspname || '.' || c.relname || ':' || COALESCE(c.relacl::text,'NULL')
       FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
       WHERE n.nspname='public' ORDER BY c.relname;
       SELECT nspname || ':' || COALESCE(nspacl::text,'NULL') FROM pg_namespace WHERE nspname='public'"
}
acls > "$work/acls.before"
docker exec -i "$RG140_POSTGRES_CONTAINER" pg_restore -U routegate -d routegate_workspace_e2e --clean --if-exists --no-owner --no-privileges --exit-on-error < "$work/database.dump"
[[ $(sql 'SELECT string_agg(version, chr(10) ORDER BY version) FROM schema_migrations') == "$ledger" ]]
[[ $(sql "SELECT has_table_privilege('rg140_workspace_runtime','public.users','INSERT')") == f ]]
# Preserve ledger readability to distinguish ACL failure from version failure.
sql "GRANT USAGE ON SCHEMA public TO rg140_workspace_runtime; GRANT SELECT ON schema_migrations TO rg140_workspace_runtime"
rc=0
ROUTEGATE_DATABASE_URL='postgres://rg140_workspace_runtime:runtime_ci_only_password@127.0.0.1:5432/routegate_workspace_e2e?sslmode=disable' \
ROUTEGATE_DATABASE_MIGRATION_MODE=external \
timeout 15s "$RUNNER_TEMP/routegate-manager" > "$work/startup.log" 2>&1 || rc=$?
[[ $rc == 1 ]]
grep -q 'requires complete runtime privileges' "$work/startup.log"
[[ $(sql "SELECT has_table_privilege('rg140_workspace_runtime','public.users','INSERT')") == f ]]
verify_identity
# Replay only the trusted fixture ACL entries, atomically, without replacing the
# cluster role or invoking ProvisionRuntimeRole's new-role-only interface.
docker cp "$work/acl.list" "$RG140_POSTGRES_CONTAINER:/tmp/rg140-fixture-acl.list"
docker exec -i "$RG140_POSTGRES_CONTAINER" pg_restore -U routegate -d routegate_workspace_e2e \
  --no-owner --single-transaction --exit-on-error --use-list=/tmp/rg140-fixture-acl.list < "$work/database.dump"
docker exec "$RG140_POSTGRES_CONTAINER" rm /tmp/rg140-fixture-acl.list
[[ $(acls) == $(cat "$work/acls.before") ]]
[[ $(sql 'SELECT string_agg(version, chr(10) ORDER BY version) FROM schema_migrations') == "$ledger" ]]
verify_identity
echo 'fixture ACLs restored with unchanged runtime identity and authentication'
