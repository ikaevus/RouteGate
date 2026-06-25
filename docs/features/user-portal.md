# RouteGate User Portal Foundation

Status: foundation

## Purpose

The User Portal is the user-facing self-service area of RouteGate. It is separate from the Admin UI and must expose only the information a VPN user needs to use their own access.

The portal is intended for:

- viewing the current user identity;
- viewing access status;
- listing VPN profiles assigned to the current user;
- opening a profile detail page;
- preparing subscription link and QR code surfaces;
- reading device setup instructions.

## Security boundary

The User Portal is not an administration interface.

Portal API routes are protected by normal authentication plus the `portal:access` permission. The built-in `vpn_user` role grants only this permission.

The first foundation implementation maps portal-visible VPN profiles by matching `vpn_accounts.email` to the authenticated user's email. A portal user can only see VPN accounts whose email matches their authenticated user email.

Portal responses must not expose:

- admin controls;
- server internals;
- agent metadata;
- private Reality keys;
- config rendering internals;
- other users' VPN accounts;
- raw subscription token hashes.

## Backend routes

The foundation adds these routes:

```text
GET /api/portal/me
GET /api/portal/dashboard
GET /api/portal/profiles
GET /api/portal/profiles/{id}
GET /api/portal/profiles/{id}/subscription
GET /api/portal/profiles/{id}/qr
GET /api/portal/instructions
GET /api/portal/instructions/{platform}
```

## Current behavior

### `GET /api/portal/me`

Returns the authenticated portal user.

### `GET /api/portal/dashboard`

Returns a small dashboard summary:

- overall access status;
- total profile count;
- active profile count;
- nearest expiration;
- notices.

### `GET /api/portal/profiles`

Returns VPN profiles assigned to the current portal user by email ownership.

### `GET /api/portal/profiles/{id}`

Returns one profile only if it belongs to the current portal user.

### `GET /api/portal/profiles/{id}/subscription`

Currently returns subscription metadata and explicitly marks the self-service subscription link as unavailable.

Reason: RouteGate stores subscription token hashes safely and does not keep raw subscription tokens. The User Portal cannot reconstruct an existing subscription URL from a stored hash. A later feature should add a deliberate self-service token issuance/rotation flow.

### `GET /api/portal/profiles/{id}/qr`

Currently returns QR metadata and explicitly marks QR rendering as unavailable until a user-facing subscription link is available.

### Instructions routes

The portal returns data-driven setup instructions for:

- `ios`
- `android`
- `windows`
- `macos`
- `linux`

The wording intentionally avoids forcing one specific third-party commercial client.

## Future work

Recommended next steps:

1. Add frontend User Portal layout and routes.
2. Add a dedicated portal login path or reuse existing auth with clear UI separation.
3. Add self-service subscription token issuance/rotation with explicit user action.
4. Render QR code after subscription link issuance exists.
5. Add traffic usage endpoint when traffic accounting exists.
6. Add backend tests for portal authorization and profile ownership.
7. Add frontend tests for portal screens.
8. Consider a stronger explicit `vpn_accounts.user_id` ownership link instead of email matching.

## Notes

This foundation intentionally avoids new database migrations. It reuses existing users, roles, permissions, auth sessions, and VPN accounts.
