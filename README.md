# RouteGate Foundation v0.1

RouteGate is a self-hosted routing and VPN management platform concept.

This archive contains the first practical project skeleton:

- Go backend: `routegate-manager`
- Go Linux agent: `routegate-agent`
- React/TypeScript frontend skeleton
- PostgreSQL dev environment
- SQL migrations
- systemd unit example
- PowerShell helper scripts
- initial architecture notes

## Repository layout

```text
routegate/
├─ backend/
├─ agent/
├─ frontend/
├─ deploy/
├─ docs/
├─ scripts/
├─ .env.example
├─ Makefile
└─ README.md
```

## Quick start

### 1. Copy environment file

```bash
cp .env.example .env
```

### 2. Start PostgreSQL

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres
```

### 3. Run Manager locally

```bash
cd backend
go mod tidy
go run ./cmd/routegate-manager
```

Healthcheck:

```bash
curl http://localhost:8080/api/admin/health
```

### 4. Run Agent locally

```bash
cd agent
go mod tidy
go run ./cmd/routegate-agent
```

## Current MVP scope

Foundation v0.1 is intentionally small:

- Manager starts HTTP API
- `/api/admin/health` works
- config is read from environment variables
- Agent has a runnable skeleton
- migration files define first database tables
- frontend has a documented empty structure

## Next recommended steps

1. Add database connection with pgx.
2. Add migration runner.
3. Implement `/api/agent/register`.
4. Implement `/api/agent/heartbeat`.
5. Add frontend Vite app.
6. Add auth tables and login flow.

