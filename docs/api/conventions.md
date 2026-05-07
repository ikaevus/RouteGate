# RouteGate API Conventions

## API zones

RouteGate API is split into explicit zones:

    /api/admin/*   Admin UI and operator API
    /api/agent/*   Agent-to-Manager API
    /api/portal/*  Future end-user portal API

## Transport

- HTTP JSON API.
- Request body format: JSON.
- Response body format: JSON.
- Timestamps: ISO 8601 / RFC3339 UTC.
- Persistent entity IDs: UUID strings.
- Development shell endpoints may temporarily use dev-only tokens or demo data.

## Headers

Request:

    Accept: application/json
    Content-Type: application/json
    Authorization: Bearer <token>
    X-Request-ID: <optional-request-id>

Response:

    Content-Type: application/json
    X-Request-ID: <request-id>

If the client provides `X-Request-ID`, the backend returns the same value. If not provided, the backend generates one.

## Success response style

Single entity:

    {
      "id": "uuid",
      "name": "Example"
    }

Collection:

    {
      "items": []
    }

Status response:

    {
      "status": "ok",
      "timestamp": "2026-01-01T00:00:00Z"
    }

## Error response style

All API errors should use the shared shape:

    {
      "status": "machine_readable_error_code",
      "message": "Human-readable message."
    }

Examples:

    {
      "status": "invalid_request",
      "message": "Request body must be valid JSON."
    }

    {
      "status": "unauthorized",
      "message": "Authentication is required."
    }

## HTTP status conventions

| HTTP status | Meaning |
|---:|---|
| 200 | Successful read/update/action |
| 201 | Entity created |
| 204 | Successful action with no body |
| 400 | Invalid request or validation error |
| 401 | Missing or invalid authentication |
| 403 | Authenticated but not allowed |
| 404 | Entity not found |
| 409 | Conflict |
| 500 | Internal server error |

## Current Foundation endpoints

Admin:

    GET  /api/admin/health
    POST /api/admin/auth/login
    POST /api/admin/auth/logout
    GET  /api/admin/me
    GET  /api/admin/servers
    POST /api/admin/servers
    GET  /api/admin/agents

Agent:

    GET  /api/agent/health
    POST /api/agent/register
    POST /api/agent/heartbeat

## Naming conventions

JSON fields use `camelCase`.

Example:

    {
      "publicIp": "203.0.113.10",
      "displayName": "RouteGate Dev Admin",
      "lastSeen": "2026-01-01T00:00:00Z"
    }

Go fields use idiomatic `PascalCase` with JSON tags.

Example:

    type Server struct {
        PublicIP string `json:"publicIp"`
    }

## Pagination convention

Not implemented yet.

Future collection endpoints should use:

    {
      "items": [],
      "page": {
        "limit": 50,
        "offset": 0,
        "total": 0
      }
    }

## Auth convention

Current Foundation auth is a development shell.

Current dev token:

    routegate-dev-token

Future production auth should replace this with one of:

    server-side sessions
    JWT access/refresh tokens
    OIDC integration

## Agent API convention

Agent API should be designed for:

    idempotent operations
    safe retries
    clear task/result lifecycle
    explicit agent identity
    minimal long-lived secrets

Future agent endpoints should use a dedicated agent token, not admin auth.