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

### Explicit SQL runtime policy (schema 175)

`routegate-manager --create-runtime-role NAME` is a separate administrative
command. It checks the migration manifest, then creates a **new NOLOGIN role**
and its grants in one transaction. It does not migrate, serve HTTP, configure
authentication, demote the legacy owner or change the service environment.
Its database connection needs role-creation and grant authority. Never supply
those credentials to the serving Manager process.

The fixed policy names all 76 current application tables. It grants ordinary
SELECT/INSERT/UPDATE/DELETE and USAGE/SELECT on their owned sequences; migration
history is SELECT-only. There is no TRUNCATE, TRIGGER, sequence UPDATE/setval,
schema/database CREATE, role membership or default grant on future objects.
Existing immutable SQL guards continue to constrain granted DML. Current
PUBLIC function permissions are not rewritten by this command.

Provisioning refuses existing roles, unexpected/missing public relations,
unsupported migration versions and unsafe PUBLIC CREATE/history-write grants.
This slice does not silently repair ambient ACLs or implement reconciliation
of an existing runtime role. Added tables require an explicit policy review;
future dispatch-consumption records must not receive ordinary runtime DML.

Disposable fixtures alone enable LOGIN with generated test credentials. They
exercise built-in RBAC initialization, schema readiness, approved and staged
revocation admission under a real runtime connection, all ten unchanged P0141
mutation denials and their complete durable snapshots. They also deny privilege
escalation, DDL, trigger disabling, TRUNCATE, ledger writes, sequence rewind and
writes to a newly created table, and check failed provisioning leaves no role.

This ordinary DML role is **not a dispatch executor** and is not protection
against all malicious application SQL: it can still write ordinary application
data, including RBAC tables. Protected dispatch authority needs its own reviewed
execution boundary. Installer credential provisioning, role upgrades, full
Manager/browser operation under restricted credentials, restore semantics and
switching the service to external mode remain required. No production role is
created or altered by this repository change alone.

### Full Manager runtime-login rehearsal

Workspace integration now runs two independent disposable PostgreSQL services:
the existing legacy startup, and external migration followed by a real runtime
login. Only the preparation step holds the fixture owner URL. Before launching
the Manager, the workflow replaces its database environment with the restricted
login and sets external migration mode. The existing full browser suite runs
unchanged in each leg, including bootstrap, authentication, server/account CRUD,
routing writes and account lifecycle UI checks. Each leg keeps its 15-minute
limit; no browser assertions are removed.

After the runtime leg, SQL readback through that same login checks persisted
application writes, role attributes, ownership and denied ledger modification,
trigger disabling and owner impersonation. Fixture LOGIN activation and its
test password exist only inside the disposable CI service. This rehearsal does
not yet provision credentials on installed hosts or prove VPN revocation E2E.

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

### External-startup ownership guard

External startup now refuses a session identity that owns the current database,
public schema or any public relation, or can become such an owner through role
membership (including NOINHERIT membership). Superuser, role/database creation,
replication and RLS-bypass roles reachable through membership are also refused.
This SELECT-only check runs before schema readiness and application startup;
errors fail closed. Legacy auto mode and explicit administrative commands retain
separate behavior. A disposable login regression checks owner rejection,
NOINHERIT owner-membership rejection and readiness after membership revocation.

This is not complete effective-privilege validation: explicit ACLs, executable
SECURITY DEFINER functions, later privilege changes and application-writable
RBAC remain separate concerns. Installer credentials, upgrade/restore role
reconciliation and protected dispatch authority are still outstanding.

### Maintenance credential reader (not connected)

A dedicated reader accepts only `/etc/routegate/database-maintenance.env` with
one `ROUTEGATE_MAINTENANCE_DATABASE_URL` entry. It treats content as data,
requires a regular root-owned 0600 file with one link, and refuses missing,
duplicate, unknown or empty entries. It never falls back to `manager.env`.
Root-run isolated tests exercise the real ownership checks; existing update
and restore callers still use their legacy connection and are unchanged.

This helper assumes trusted path ancestry and no concurrent host mutation;
it does not establish a race-free filesystem boundary or validate SQL grants.
Do not connect it to privileged execution until installer provisioning,
credential lifecycle, backup/restore handling and trusted path validation are
implemented together. No maintenance credential is generated by this slice.

### Required runtime grants at external startup

External startup now checks the explicit application's required table DML and
owned-sequence USAGE/SELECT privileges after ownership and migration readiness.
It also requires database CONNECT, public schema USAGE and ledger SELECT. Every
privilege is checked individually: PostgreSQL's comma-separated privilege list
means ANY, not ALL. Missing policy tables fail closed. Checks issue SELECT only,
never restore ACLs or retry with administrative credentials.

This prevents a current migration ledger from admitting a runtime login after
application grants have been lost, including the ACL-loss condition that restore
with `--no-privileges` must handle. Tests revoke each of the 304 required table
privilege bits independently, verify refusal without repair, restore each grant
and verify readiness again under a read-only runtime session. Sequence privilege
loss, a missing policy table and complete application-table ACL loss are covered.
This is an ACL-loss fixture, not a full pg_dump/pg_restore rehearsal.

A ledger-only login can still inspect schema versions through CheckSchema, but
can no longer start the serving Manager in external mode. This is a minimum
privilege availability check, not a complete effective-privilege audit: excessive
ACLs, SECURITY DEFINER functions, RLS and later grant changes remain separate
concerns. Administrative reconciliation, credential provisioning and restore
rehearsal remain required before enabling the installed runtime role.
