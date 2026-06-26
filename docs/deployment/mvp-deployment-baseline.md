# MVP Deployment Baseline

This document is the first release-readiness baseline for bringing up RouteGate on a single node.

RouteGate is an independent Linux VPN management platform. This guide keeps the MVP deployment simple and focused on a single VPS or development host.

## Current deployment profile

The current repository supports a development-oriented Docker Compose stack:

- PostgreSQL 16
- RouteGate Manager API
- RouteGate Admin UI through Vite

The current stack is suitable for development and early MVP validation. It is not a hardened production deployment yet.

## Prerequisites

Install these tools on the host:

- Git
- Docker
- Docker Compose plugin
- Go 1.25+ for local backend and agent checks
- Node.js 24+ and npm for frontend checks

Verify the basic tooling:

```bash
git --version
docker --version
docker compose version
go version
node --version
npm --version
```

## Clone the repository

```bash
git clone https://github.com/ikaevus/RouteGate.git
cd RouteGate
```

For private repository access, use the GitHub authentication method already configured for the deployment operator.

## Configure environment

Create a local environment file from the example:

```bash
cp .env.example .env
```

Before using a shared, public, or production-like host, replace every development placeholder in `.env`:

- `ROUTEGATE_DATABASE_URL`
- `ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL`
- `ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME`
- `ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD`
- `POSTGRES_PASSWORD`
- agent registration and credential placeholders if an agent is being prepared

Do not commit `.env` or real secrets.

The current `deploy/docker-compose.dev.yml` file contains development defaults directly in the compose file. Keep `.env.example` as the canonical list of MVP deployment variables while the deployment model is still being stabilized.

## Start the development stack

```bash
make dev
```

Equivalent direct command:

```bash
docker compose -f deploy/docker-compose.dev.yml up --build
```

This starts:

- `routegate-postgres-dev`
- `routegate-manager-dev`
- `routegate-frontend-dev`

Check containers:

```bash
make ps
```

Follow logs:

```bash
make logs
```

Stop the stack:

```bash
make down
```

## Database migrations

Migrations live in:

```text
backend/migrations
```

The Manager applies migrations during startup in the current development stack. To verify migration behavior, watch the Manager logs during startup:

```bash
docker compose -f deploy/docker-compose.dev.yml logs -f manager
```

If the development database becomes disposable or inconsistent, reset it with:

```bash
make db-reset
make dev
```

`make db-reset` removes the development database volume. Do not use it against data you need to keep.

## Health checks

Check the Admin UI:

```text
http://127.0.0.1:5173
```

Check Manager health directly:

```bash
curl -i http://127.0.0.1:8080/api/admin/health
```

Check Manager health through the frontend dev proxy:

```bash
curl -i http://127.0.0.1:5173/api/admin/health
```

Check Agent API health:

```bash
curl -i http://127.0.0.1:8080/api/agent/health
```

Check PostgreSQL readiness through Docker:

```bash
docker compose -f deploy/docker-compose.dev.yml exec postgres pg_isready -U routegate -d routegate
```

## Default development login

The development stack uses bootstrap admin values from the compose file.

Default local credentials:

```text
email:    admin@routegate.local
password: admin
```

Use these only for local development. Change bootstrap values before any shared or public deployment.

## Useful Makefile commands

```bash
make help
make dev
make down
make restart
make rebuild
make logs
make ps
make backend-test
make agent-test
make frontend-build
make check
make db-reset
make clean
```

## Basic troubleshooting

### Port already in use

The development stack uses ports `5173`, `8080`, and `5432`. Stop the conflicting service or change the compose port mapping.

### PostgreSQL is not ready

Check the PostgreSQL container and health check:

```bash
make ps
docker compose -f deploy/docker-compose.dev.yml logs postgres
```

### Manager cannot connect to database

Verify that `ROUTEGATE_DATABASE_URL` matches the PostgreSQL host, database, user, password, and port used by the active deployment.

Inside Docker Compose, the database host is `postgres`. From the host shell, the database host is usually `localhost`.

### Frontend cannot reach API

Check Manager health directly first:

```bash
curl -i http://127.0.0.1:8080/api/admin/health
```

Then check the Vite proxy path:

```bash
curl -i http://127.0.0.1:5173/api/admin/health
```

### Disposable dev database reset

Only for local development:

```bash
make db-reset
make dev
```

## Minimal MVP deployment acceptance checks

Before calling the MVP deployment baseline ready, verify:

```bash
make check
make dev
curl -i http://127.0.0.1:8080/api/admin/health
curl -i http://127.0.0.1:8080/api/agent/health
curl -i http://127.0.0.1:5173/api/admin/health
```

Then open:

```text
http://127.0.0.1:5173
```

Log in with the configured bootstrap admin account.
