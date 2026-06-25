# RG-44 VLESS / Reality Admin API

Status: Foundation

RG-44 exposes the VLESS / Reality credential foundation added in RG-43 through authenticated Admin API endpoints.

## Server Protocol Settings

Read server protocol settings:

```http
GET /api/v1/servers/{server_id}/protocol-settings
Authorization: Bearer <admin-session-token>
```

Required permission: `servers:read`

Update server protocol settings:

```http
PATCH /api/v1/servers/{server_id}/protocol-settings
Authorization: Bearer <admin-session-token>
Content-Type: application/json
```

Required permission: `servers:update`

Request body fields are optional:

```json
{
  "vlessPort": 443,
  "vlessFlow": "xtls-rprx-vision",
  "vlessNetwork": "tcp",
  "realityPublicKey": "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
  "realityShortId": "0123456789abcdef",
  "realityServerName": "www.example.com"
}
```

Response:

```json
{
  "serverId": "server-uuid",
  "protocol": "vless",
  "vless": {
    "port": 443,
    "flow": "xtls-rprx-vision",
    "network": "tcp"
  },
  "reality": {
    "enabled": true,
    "publicKey": "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
    "shortId": "0123456789abcdef",
    "serverName": "www.example.com"
  },
  "updatedAt": "2026-06-25T12:00:00Z"
}
```

## VPN Account Credentials

Read the admin-visible VLESS / Reality profile for a VPN account:

```http
GET /api/v1/vpn-accounts/{id}/credentials
Authorization: Bearer <admin-session-token>
```

Required permission: `vpn_users:read`

Response:

```json
{
  "vpnAccountId": "account-uuid",
  "serverId": "server-uuid",
  "endpoint": "fi.routegate.example",
  "protocol": "vless",
  "vless": {
    "uuid": "9d0f2e47-0f70-4b8d-9f12-8a2324c0bb5e",
    "flow": "xtls-rprx-vision",
    "network": "tcp"
  },
  "reality": {
    "enabled": true,
    "publicKey": "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
    "shortId": "0123456789abcdef",
    "serverName": "www.example.com"
  }
}
```

## Scope Notes

This foundation does not generate Reality keypairs, does not store private Reality keys, and does not add Admin UI screens yet.
