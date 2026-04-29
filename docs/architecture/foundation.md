# RouteGate Foundation Architecture

## Decision

RouteGate starts as a modular monolith with a monorepository layout.

## Components

- Manager: central Go backend and API server.
- Agent: Linux Go agent for node operations.
- Frontend: React/TypeScript admin UI.
- PostgreSQL: primary persistent storage.

## API zones

- `/api/admin/*` for admin UI.
- `/api/agent/*` for agent communication.
- `/api/portal/*` is reserved for future user portal.

## First milestone

The first milestone is not VPN functionality. The first milestone is a living system spine:

1. Manager starts.
2. Database exists.
3. Healthcheck works.
4. Frontend shell opens.
5. Agent starts.
6. Agent heartbeat can be implemented next.
