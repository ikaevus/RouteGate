# Admin API Draft

RouteGate Manager API uses opaque bearer tokens for authenticated Manager requests. Public endpoints are limited to health checks and login.

## Authentication

### Bootstrap first SuperAdmin

On backend startup, migrations are applied and built-in permissions/roles are seeded. If no `super_admin` user exists, set these environment variables before starting the manager:

| Variable | Required | Description |
| --- | --- | --- |
| `ROUTEGATE_BOOTSTRAP_ADMIN_EMAIL` | Yes | Email for the first SuperAdmin. |
| `ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD` | Yes | Initial password; it is hashed before storage and never logged. |
| `ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME` | No | Optional username for login. |
| `ROUTEGATE_BOOTSTRAP_ADMIN_DISPLAY_NAME` | No | Optional display name. |
| `ROUTEGATE_AUTH_SESSION_TTL_HOURS` | No | Opaque session/token lifetime in hours. Defaults to `24`. |

If a SuperAdmin already exists, startup does not create another one automatically.

### Login

```http
POST /api/v1/auth/login
Content-Type: application/json
```

Compatibility alias:

```http
POST /api/admin/auth/login
```

Request:

```json
{
  "login": "admin@example.com",
  "password": "secret"
}
```

`login` accepts either email or username. The legacy `email` field is also accepted for the `/api/admin/auth/login` frontend compatibility alias.

Response:

```json
{
  "token": "opaque-token-returned-once",
  "expires_at": "2026-05-08T12:00:00Z",
  "user": {
    "id": "uuid",
    "email": "admin@example.com",
    "username": "admin",
    "display_name": "RouteGate Admin",
    "user_type": "human",
    "status": "active",
    "roles": ["super_admin"],
    "permissions": ["system:manage", "users:read"]
  }
}
```

Failed login returns `401` with a generic error. Disabled, locked, and pending users cannot log in.

### Authenticated requests

```http
Authorization: Bearer <token>
```

Tokens are opaque random values. The database stores only a SHA-256 token hash in `auth_sessions`.

### Current user

```http
GET /api/v1/auth/me
Authorization: Bearer <token>
```

Compatibility alias:

```http
GET /api/admin/me
```

### Logout

```http
POST /api/v1/auth/logout
Authorization: Bearer <token>
```

Compatibility alias:

```http
POST /api/admin/auth/logout
```

Revokes the current token/session.

## Users

All users endpoints require bearer authentication. Role assignment in create/update additionally requires `roles:assign`.

```http
GET   /api/v1/users              # users:read
GET   /api/v1/users/{id}         # users:read
POST  /api/v1/users              # users:create
PATCH /api/v1/users/{id}         # users:update
POST  /api/v1/users/{id}/disable # users:disable
POST  /api/v1/users/{id}/enable  # users:disable
```

Create request:

```json
{
  "email": "operator@example.com",
  "username": "operator",
  "password": "secret",
  "display_name": "Operator",
  "user_type": "human",
  "status": "active",
  "roles": ["operator"]
}
```

The API prevents disabling the last active SuperAdmin and prevents removing the last SuperAdmin role from the last SuperAdmin account.

## Roles and permissions

```http
GET /api/v1/roles       # roles:read
GET /api/v1/permissions # roles:read
```

Built-in roles seeded by the backend:

| Code | Name | Permission intent |
| --- | --- | --- |
| `super_admin` | SuperAdmin | Full access to all permissions. |
| `admin` | Admin | Operational Manager access except `system:manage`, `licenses:manage`, portal, and agent runtime permissions. |
| `operator` | Operator | Day-to-day operational access. |
| `read_only` | ReadOnly | Read-only Manager access. |
| `vpn_user` | VpnUser | `portal:access`. |
| `agent` | Agent | Agent runtime permissions only. |

## Manager resources

Legacy server compatibility routes remain available for older Manager surfaces:

```http
GET  /api/admin/servers
POST /api/admin/servers
```

Permission-protected v1 endpoints are the canonical Manager API:

```http
GET  /api/v1/servers # servers:read
POST /api/v1/servers # servers:create
GET  /api/v1/agents  # agents:read
GET  /api/v1/system/version # agents:read
```

`GET /api/v1/system/version` returns Manager, Web UI, database schema, Agent protocol compatibility, and manual-update metadata. It does not perform update network calls.

Example response:

```json
{
  "manager": {
    "version": "dev",
    "gitCommit": "unknown",
    "buildDate": "unknown"
  },
  "webUi": {
    "version": "dev"
  },
  "database": {
    "expectedSchemaVersion": 102,
    "appliedSchemaVersion": "000102_agent_protocol_version"
  },
  "agentCompatibility": {
    "protocolVersion": 1,
    "minimumProtocolVersion": 1,
    "recommendedAgentVersion": "dev"
  },
  "update": {
    "status": "manual",
    "channel": "development",
    "automaticUpdatesSupported": false
  }
}
```

Agent list responses include `agentVersion`, `protocolVersion`, and `compatibility`. Agents that have not reported protocol metadata are classified as `unknown`.

The Manager health endpoint remains public:

```http
GET /api/admin/health
```

The Agent runtime API is documented separately in `agent-api.md` and uses only `/api/v1/agent/*` endpoints.

## Example curl flow

```bash
curl -sS -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"login":"admin@example.com","password":"secret"}'
```

```bash
TOKEN='<token from login response>'
curl -sS http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer ${TOKEN}"
```

```bash
curl -sS http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer ${TOKEN}"
```

```bash
curl -sS -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer ${TOKEN}"
```
