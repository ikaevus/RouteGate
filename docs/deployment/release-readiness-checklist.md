# Release Readiness Checklist

This checklist defines the minimum release discipline for RouteGate MVP work.

Use it before creating an MVP tag, publishing a deployment note, or asking another person to test a release candidate.

## Scope

This checklist is for the current single-node MVP profile:

- RouteGate Manager
- RouteGate Admin UI
- PostgreSQL
- RouteGate Agent compatibility notes
- Docker Compose based deployment baseline

It does not cover Kubernetes, HA, marketplace images, appliance OS images, or a full auto-update system.

## Pre-release checks

Run the full project check:

```bash
make check
```

If investigating failures separately, run:

```bash
make backend-test
make agent-test
make frontend-build
```

The release candidate should not proceed while any required check is failing.

## Frontend build

Confirm that the Admin UI production build succeeds:

```bash
make frontend-build
```

Do not commit generated frontend build output such as `frontend/dist`.

## Database migrations

Before release:

- Confirm every schema change has an up migration.
- Confirm rollback expectations are documented when a down migration is risky or not enough.
- Start the Manager against a disposable database and verify migrations are applied on startup.
- Never test destructive migration behavior against data that must be preserved.

Migration location:

```text
backend/migrations
```

## Environment validation

Check `.env.example` when adding or changing configuration.

The example file must:

- list the required Manager variables;
- list relevant PostgreSQL variables;
- list Agent variables used by current examples or packaging work;
- avoid real secrets;
- make development-only values obvious;
- make production-like placeholders obvious.

Before a public or shared deployment, verify that development credentials were replaced.

## Backup before update

Before updating an existing MVP deployment, take a PostgreSQL backup.

Example for the current development compose profile:

```bash
mkdir -p backups
docker compose -f deploy/docker-compose.dev.yml exec -T postgres \
  pg_dump -U routegate -d routegate > backups/routegate-$(date +%Y%m%d-%H%M%S).sql
```

For a production-like deployment, adapt the command to the real PostgreSQL host, database, and credentials.

A backup is only useful if restore has been tested on a disposable database.

## Basic restore drill

For MVP release readiness, restore should be tested in a disposable environment before relying on a backup procedure.

High-level restore flow:

1. Stop services that write to the database.
2. Create or reset a disposable PostgreSQL database.
3. Restore the SQL dump.
4. Start the Manager.
5. Verify health endpoints and the Admin UI.
6. Confirm core records are present.

Do not perform restore drills on the only copy of useful data.

## Rollback notes

For each release candidate, document:

- previous known-good commit or tag;
- database migration impact;
- whether database rollback is safe, manual, or not supported;
- frontend rollback command or container/image reference when packaging exists;
- Manager rollback command or container/image reference when packaging exists;
- Agent compatibility expectations.

For the current repository baseline, rollback usually means returning to the previous Git commit and restarting the compose stack. Database rollback must be considered separately.

## Manager / Agent compatibility

Before release, note whether the Manager and Agent must be upgraded together.

Minimum compatibility notes:

- Agent registration payload compatibility
- Agent heartbeat payload compatibility
- Config apply job API compatibility
- Traffic usage payload compatibility
- Client config or subscription payload compatibility when relevant

Avoid silently breaking older agents. If compatibility is intentionally broken, document it in the release note.

## Health checks after update

After updating a deployment, verify:

```bash
curl -i http://127.0.0.1:8080/api/admin/health
curl -i http://127.0.0.1:8080/api/agent/health
curl -i http://127.0.0.1:5173/api/admin/health
```

Also verify the Admin UI manually:

```text
http://127.0.0.1:5173
```

## MVP release candidate checklist

A release candidate is ready for review when all items below are true:

- [ ] `make check` passes.
- [ ] Backend tests pass.
- [ ] Agent tests pass.
- [ ] Frontend build passes.
- [ ] Migrations are present and startup migration behavior is verified.
- [ ] `.env.example` is current and contains no real secrets.
- [ ] Deployment docs are current.
- [ ] Release notes include known limitations.
- [ ] Backup guidance is present.
- [ ] Rollback notes are present.
- [ ] Manager / Agent compatibility is documented.
- [ ] Health checks pass after startup.
- [ ] Admin UI opens and login works with configured credentials.

## Known MVP limitations to keep visible

Keep the release note honest about incomplete areas. Depending on the current milestone, these may include:

- development-oriented compose stack;
- development bootstrap credentials unless replaced;
- incomplete production hardening;
- incomplete automated update system;
- incomplete appliance image flow;
- incomplete HA story.
