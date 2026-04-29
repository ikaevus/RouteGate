# Admin API Draft

## Health

```http
GET /api/admin/health
```

Expected response:

```json
{
  "status": "ok",
  "service": "routegate-manager",
  "timestamp": "2026-04-29T00:00:00Z"
}
```

## Reserved future groups

```text
POST   /api/admin/auth/login
GET    /api/admin/me
GET    /api/admin/servers
POST   /api/admin/servers
GET    /api/admin/agents
GET    /api/admin/vpn-accounts
GET    /api/admin/routing-profiles
GET    /api/admin/audit-log
```
