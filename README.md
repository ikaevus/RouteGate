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
    make frontend-i18n-check
    make frontend-build
    make check
    make db-reset
    make dev-traffic-usage
    make clean

Common workflow:

    make check
    make dev

Frontend localization QA:

    docs/development/frontend-i18n-check.md

Traffic usage dev scenario:

    docs/features/traffic-usage-e2e-dev-scenario.md

Stop stack:

    make down

Reset development database volume:

    make db-reset

## MVP deployment and release readiness

MVP deployment baseline:

    docs/deployment/mvp-deployment-baseline.md

Release readiness checklist:

    docs/deployment/release-readiness-checklist.md

MVP v0.1 acceptance pass:

    docs/release/mvp-v0.1-acceptance-pass.md

These documents cover the current single-node MVP path: environment variables, Docker Compose startup, migration verification, health checks, backup guidance, rollback notes, Manager / Agent compatibility reminders, and the full MVP v0.1 acceptance pass.

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
