# RG-140 database role separation: migration entry point

Status: first implementation slice; NOT a deployed privilege boundary.
Base: #634, `15e2ccb344eebd89ebf672335a8fe004c04bdafd`. #545 stays OPEN.

The Manager previously always migrated through its serving connection. A
non-owner runtime login cannot use that startup path, even on a current schema,
because the migrator executes CREATE TABLE IF NOT EXISTS.

## Implemented

- `routegate-manager --migrate-only` applies migrations, checks the complete
  version manifest, closes its connection and exits. It does not bootstrap an
  administrator, start workers, recover jobs, backfill settings or serve HTTP.
- `ROUTEGATE_DATABASE_MIGRATION_MODE=external` makes normal startup SELECT-only
  for schema readiness, before any bootstrap/workers/HTTP. Missing history,
  missing older migrations, unexpected versions and empty manifests fail closed.
  There is no fallback to automatic migration after a failed check.
- `auto` remains the legacy default until installation and upgrade provisioning
  are ready. An unknown nonempty mode is rejected. Programmatic empty Config
  values preserve legacy behavior.
- PostgreSQL tests authenticate a separate login with only schema USAGE and
  migration-history SELECT. Readiness works, while DDL, ledger deletion,
  trigger disabling and SET ROLE to the owner are denied with SQLSTATE 42501.

Version readiness does not attest migration file contents, actual schema
integrity, runtime privileges, or successful VPN revocation. Runtime connections
must not receive migrator credentials. The external mode is not by itself a
security boundary, and the fixture's read-only login is not a complete Manager
application role.

## Following slices, required before enabling scoped dispatch

### Migration file ownership prerequisite

The next slice makes `/opt/routegate-manager` and its migrations root-owned
(directories 0755, SQL files 0644) on clean installation, both Manager update
paths, and both backup-restore paths. Legacy archive ownership and permissive
modes are normalized. The former `RG_UPDATE_MANAGER_OWNER` override no longer
controls migration ownership. Symlinks, non-regular entries and hard-linked
files are rejected before recursive ownership changes.

An isolated root-only filesystem test uses a real unprivileged identity to
prove readable SQL but denied modification, creation, chmod and directory
replacement, including after restoring an old archive. It also checks linked
entries fail without changing an outside fixture. The existing update suites
remain unchanged. This does not provision SQL roles or enable external mode.

These helpers assume stopped services and a trusted release/backup tree, as do
their existing callers. They are post-install normalization, not an atomic
defence against a concurrently compromised host. Before privileged migration
execution is enabled, staging, path ancestry, credentials, service confinement
and backup trust must be reviewed together. No privileged migration execution
is introduced by this ownership slice.

### Remaining work

1. Provision separate owner/migrator and runtime roles; never grant owner
   membership or schema CREATE to runtime. Determine explicit DML/function
   grants, including trigger-written audit rows and sequences. Do not broadly
   grant future protected dispatch records via default privileges.
2. Invoke migrations out of process in installation and updates, with credentials
   inaccessible to the Manager service. Set external mode only after migration
   and privilege validation succeed. Inventory ownership of files/service units
   as well as SQL objects: process separation alone is insufficient.
3. Rehearse clean install, existing-owner conversion, interrupted upgrade,
   backup/restore and binary rollback. External startup intentionally rejects a
   database with newer migration versions. Do not silently fall back to owner
   credentials to make rollback appear healthy.
4. Prove representative Manager behavior under the restricted runtime login,
   and deny direct writes to dedicated dispatch authority. Only then implement
   the atomic operation-owned dispatch/claim/completion/recovery transitions.

This slice changes no SQL trigger, installer provisioning, service credential,
or production host. It creates no executable removal job or new evidence ledger.
