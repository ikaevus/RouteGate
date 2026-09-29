# Admin API Draft

RouteGate Manager API uses opaque bearer tokens for authenticated Manager requests. Public endpoints are limited to health checks and login.

## VPN protocol settings

`GET` and `PATCH /api/v1/servers/{server_id}/protocol-settings` expose the
selected server protocol plus separate VLESS/Reality, WireGuard, Hysteria2,
Shadowsocks, and MTProto
settings. The accepted values are `vless`, `wireguard`, `hysteria2`,
`shadowsocks`, and `mtproto`. Hysteria2 adds:

- `hysteria2.port`
- `hysteria2.domain`
- `hysteria2.acmeEmail`
- `hysteria2.masqueradeUrl` (fixed safe target)
- `hysteria2.ready`

RG-114E adds:

- `wireGuard.port`
- `wireGuard.address`
- `wireGuard.dns`
- `wireGuard.publicKey`
- `wireGuard.ready`

`POST /api/v1/servers/{server_id}/protocol-settings/wireguard-recommended`
generates a new server keypair and selects the recommended native WireGuard
settings. The server private key is never returned by the Admin API.

`POST /api/v1/servers/{server_id}/protocol-settings/recommended` configures
VLESS / Reality. It requires JSON `{"serverName":"www.example.com"}` with an
explicit external TLS handshake hostname. RouteGate rejects an IP address or
the node's own hostname; `PATCH /api/v1/servers/{server_id}/protocol-settings`
applies the same node-hostname rule to `realityServerName`. The Manager
validates syntax only. The new keypair, Short ID, hostname and VLESS settings
are saved in one atomic database update. Saving settings does not apply a
runtime configuration.

Client material (subscriptions and connection links) always describes the
node's last Agent-confirmed apply. Each config version records the
client-facing parameters it deploys, derived from its own rendered config:
node protocol, ports, VLESS flow and transport, Reality public key (derived
from the deployed private key), Short ID and server name, and the
WireGuard/Hysteria2/Shadowsocks/MTProto endpoint parameters. Saved settings
reach clients only after a version that contains them is applied
successfully; a failed or rejected apply leaves clients on the previous
parameters, and re-applying an older version switches clients back to that
version. Until a node has any successful apply, client connection endpoints
return `client_connection_unavailable`. The WireGuard DNS pushed to clients
is not part of the node runtime and takes effect immediately.

Each config version also records, per rendered account, the protocols it
deploys and the primary protocol chosen at render time. A successful apply
sets every listed account's active protocol set and primary protocol exactly
to that version, so re-applying an older version rolls clients back to its
protocols, and a preference saved after the render stays pending. A client
profile created later starts from the active version's protocols instead of
defaulting to VLESS.

An account the active version does not list (created, activated or given a
protocol after that render) has no credentials on the node. Every client
delivery path (the connection endpoint, token subscriptions, devices and each
member of a multi-protocol set) then returns `client_connection_unavailable`
naming the next action: render and successfully apply a new configuration.
A multi-protocol set is served whole or not at all. Saving the account's
client profile still works; the response then carries the profile without
links, and preferences are validated against the node's saved settings.

MTProto is the exception: its proxy uses one node-wide secret and the render
does not list MTProto accounts. MTProto material is served whenever the
account's MTProto protocol is active and the applied version runs the MTProto
proxy, so existing MTProto clients keep working; an apply without the proxy
deactivates MTProto for all accounts of the node.

Compatibility limit: a version whose rendered config does not list its
accounts (renders from before per-account protocol lists, or an entry
without an account or protocol) records `accounts: null`. For such a version
Manager cannot tell deployed from undeployed accounts; client material then
follows the active protocol flags as before, and the per-account check starts
with the next successful apply of a version rendered by this Manager.

`vlessNetwork` accepts only `tcp`: the managed VLESS / Reality inbound serves
raw TCP. A per-client Reality server name override is used only when it
matches the node's applied server name (ignoring case); any other value would
fail every Reality handshake.

Versions rendered before migration `000156` get their snapshot when Manager
starts: it derives the parameters from each stored rendered config and logs
`backfilled applied client settings`. A version that cannot be derived is
logged as an error with its `config_version_id` and keeps serving the node's
saved settings until the next successful apply. When a Reality config is applied, Agent resolves the
handshake target and completes a TLS 1.3 handshake from the VPN node before
replacing the running config; if that fails, the apply job fails at the
`validate` stage and the running VPN service is left unchanged.

VPN account credential and token-protected subscription responses select their
shape from the assigned server protocol. WireGuard delivery returns the peer
private/public keys, assigned address, server public key, DNS value, and a
standard importable config. These responses grant VPN access and must be
handled like the existing VLESS credential and QR responses.

Hysteria2 delivery returns the UUID-derived username, random account password,
dedicated TLS domain and UDP port, plus a standard `hysteria2://` URI. These
credentials receive the same access controls as the VLESS and WireGuard paths.

Shadowsocks protocol settings expose only port, fixed method, and readiness;
the server PSK is intentionally omitted. Protected account credentials include
the server and user PSKs, while client connection and subscription responses
provide a standard `ss://` URI. MTProto settings expose port, fixed FakeTLS
domain, and readiness but not the secret. Protected credentials mark the secret
as node-shared, and client responses provide a standard `tg://proxy` URI.

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

Server inventory entries are RouteGate nodes. `deploymentRole` is one of
`management`, `vpn`, or `hybrid`. Older clients may omit it when creating a
record; Manager then creates a VPN Node.

```json
{
  "name": "us-vpn-01",
  "deploymentRole": "vpn",
  "publicIp": "203.0.113.10"
}
```

The Clean VPS All-in-One installer explicitly creates a Hybrid Node. A
Management-only node cannot receive an Agent registration token or VPN config.

Canonical `GET /api/v1/servers` and `GET /api/v1/servers/{server_id}` responses
also include an `inventory` summary derived by Manager from the assigned role,
heartbeat freshness, Agent protocol compatibility, and reported capabilities:

```json
{
  "connectionState": "online",
  "capabilityStatus": "compatible",
  "nextAction": "none",
  "capabilitySchemaVersion": 1,
  "managedAdapterCount": 1
}
```

For a VPN or Hybrid Node, `POST
/api/v1/servers/{server_id}/registration-token` returns the raw one-time token
once and, when `ROUTEGATE_PUBLIC_URL` is a valid HTTPS origin, a complete
`bootstrapCommand`. The command installs only RouteGate Agent on the remote
node. Management Nodes receive `409 node_role_incompatible`.

`GET /api/v1/system/version` returns Manager, Web UI, database schema, Agent protocol compatibility, and manual-update metadata. It does not perform update network calls.

### Ordered VPN-node update rollouts

The administrator-reachable rolling-update boundary exposes only Manager-owned
rollout state and composes the existing verified single-node update lifecycle.
It does not accept Agent identities, release locations, artifacts, commands,
paths, trust roots, retry settings, rollback settings, or parallelism controls.

```http
POST /api/v1/platform-update-rollouts                       # system:manage
GET  /api/v1/platform-update-rollouts/{rollout_id}          # servers:read
POST /api/v1/platform-update-rollouts/{rollout_id}/advance  # system:manage
```

Creation requires a canonical UUIDv4 `Idempotency-Key`, a body no larger than
64 KiB, one canonical target version, and between 1 and 1024 ordered canonical
server UUIDs:

```json
{
  "targetVersion": "v1.2.3",
  "serverIds": [
    "550e8400-e29b-41d4-a716-446655440001",
    "550e8400-e29b-41d4-a716-446655440002"
  ]
}
```

An identical replay with the same key returns the original rollout. Reusing the
key for a different canonical request returns `409` and creates no rollout.
Creation records an immutable planning snapshot only; it does not create or
admit a host-update job.

GET returns at most 1024 ordered entries from one coherent database snapshot,
including bounded durable `errorCode` and `blockerCode` values. It never returns
the creation key, credentials, updater output, local paths, commands, or trust
material.

Advance requires a completely empty request body and invokes the rollout step
controller exactly once. One request may admit at most one node mutation or
prove the current node healthy, never both. After `node_healthy`, admitting the
next node requires a later explicit administrator request. Failed and
`outcome_unknown` rollouts remain stopped for operator inspection.

Allow-listed node diagnostics are queued through:

```http
POST /api/v1/servers/{server_id}/diagnostics
GET  /api/v1/servers/{server_id}/diagnostics
GET  /api/v1/servers/{server_id}/diagnostics/{run_id}
```

The create body contains one registered `profileKey`: `host_overview`,
`vpn_core_status`, or `manager_certificate`. Manager queues the operation only
when the attached Agent advertised that exact profile. Agent evidence is
treated as untrusted input; Manager derives state, reason code, summary, and
recommended action before storing the diagnostic result.

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
    "expectedSchemaVersion": 124,
    "appliedSchemaVersion": "000124_node_deployment_roles"
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

RG-114H adds node group CRUD/member/candidate endpoints and account routing
policy endpoints. Candidate results are derived, read-only Manager state;
`automaticSelection` remains false and no endpoint in this slice changes an
account's concrete server assignment implicitly.

RG-114I adds per-account automatic-selection policy, deterministic preview, and
operator-triggered apply endpoints. Apply returns the previous/selected server
IDs plus the exact affected-node set that requires config render/deploy.
