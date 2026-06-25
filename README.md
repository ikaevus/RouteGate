# RouteGate

RouteGate is a self-hosted VPN and routing management platform concept.

The current repository contains the Foundation implementation for the RouteGate Manager, Agent shell, Admin UI, PostgreSQL persistence, and local developer environment.

## Current status

Foundation is active and includes:

- Go backend Manager API.
- React / TypeScript Admin UI.
- PostgreSQL persistence.
- Docker Compose development stack.
- Server registry shell.
- Agent registry and heartbeat shell.
- Development auth shell.
- Dashboard overview.
- Request ID middleware.
- Panic recovery middleware.
- Development CORS middleware.
- Shared JSON response helpers.
- API conventions document.
- OpenAPI seed.
- Developer Makefile commands.

## Architecture

Current Foundation stack:

    Browser
      |
      v
    RouteGate Frontend :5173
      |
      v
    Vite dev proxy /api/*
      |
      v
    RouteGate Manager :8080
      |
      v
    PostgreSQL :5432

Main repository areas:

    backend/    Go RouteGate Manager API
    agent/      Go RouteGate Agent skeleton
    frontend/   React / TypeScript Admin UI
    deploy/     Docker Compose and deployment files
    docs/       Architecture, API, operations and decision notes
    scripts/    Helper scripts

## Requirements

For local development:

- Git
- Go 1.25+
- Node.js 24+
- npm
- Docker
- Docker Compose
- VS Code recommended

The backend Docker image currently uses Go 1.26.

## Quick start

Start the full development stack:

    make dev

This starts:

    routegate-postgres-dev
    routegate-manager-dev
    routegate-frontend-dev

Open the Admin UI:

    http://127.0.0.1:5173

Manager health through Vite proxy:

    curl -i http://127.0.0.1:5173/api/admin/health

Direct Manager health:

    curl -i http://127.0.0.1:8080/api/admin/health

## Developer commands

    make help
    make dev
    make down
    make restart
    make rebuild
    make logs
    make ps
    make backend-test
    make agent-test
    make frontend-install
    make frontend-build
    make check
    make db-reset
    make dev-traffic-usage
    make clean

Common workflow:

    make check
    make dev

Traffic usage dev scenario:

    docs/features/traffic-usage-e2e-dev-scenario.md

Stop stack:

    make down

Reset development database volume:

    make db-reset

## Ports

| Service | Port | Description |
|---|---:|---|
| Frontend | 5173 | Vite dev server |
| Manager API | 8080 | Go backend API |
| PostgreSQL | 5432 | Development database |

## Development auth shell

The current authentication flow is a Foundation placeholder.

Login page:

    http://127.0.0.1:5173/login

Default dev credentials:

    email:    admin@routegate.local
    password: admin

Current dev token:

    routegate-dev-token

Production authentication is not implemented yet.

## API endpoints

Current Foundation endpoints:

    GET  /api/admin/health
    POST /api/admin/auth/login
    POST /api/admin/auth/logout
    GET  /api/admin/me

    GET  /api/admin/servers
    POST /api/admin/servers

    GET  /api/admin/agents

    GET  /api/agent/health
    POST /api/agent/register
    POST /api/agent/heartbeat

API documentation:

    docs/api/conventions.md
    docs/api/openapi.yaml

## Backend structure

The backend uses a modular monolith structure.

Typical module layout:

    handler.go      HTTP request/response layer
    service.go      validation and business flow
    repository.go   PostgreSQL access
    model.go        domain model
    dto.go          API DTOs

Implemented modules using this pattern:

    backend/internal/servers
    backend/internal/agents

Shared backend packages:

    backend/internal/config
    backend/internal/db
    backend/internal/http
    backend/internal/httpx
    backend/internal/health
    backend/internal/auth

## Database and migrations

PostgreSQL is used as the main database.

Migrations live in:

    backend/migrations

The Manager applies migrations at startup in the development stack.

Current migration files:

    000001_init.up.sql
    000001_init.down.sql
    000002_agent_identity.up.sql
    000002_agent_identity.down.sql

## Current Foundation milestones

Completed:

    MVP-0: dev stack
    MVP-1: auth shell
    MVP-2: server registry shell
    MVP-3: agent registry shell
    MVP-4: dashboard overview
    MVP-5: PostgreSQL persistence
    MVP-6: repository layer cleanup
    MVP-7: service layer
    MVP-8: shared HTTP response helpers
    MVP-9: middleware stack
    MVP-10: API conventions + OpenAPI seed
    MVP-11: developer Makefile commands
    MVP-12: README refresh

## Next likely workstreams

Recommended next workstreams:

    Auth / Users / Roles
    Server Registry / Agents
    Config Render / Validate / Apply / Rollback
    Agent runtime implementation
    Audit Log
    Routing Profiles / Split Tunnel
    VPN Accounts / Subscriptions / QR

## Notes

The current implementation is still Foundation-level.

Important limitations:

- Auth is development-only.
- Agent token handling is development-only.
- Server and agent APIs are minimal.
- No production RBAC yet.
- No audit log writes yet.
- No config rendering or apply workflow yet.
- No real sing-box integration yet.
