# RBAC Admin Sessions Baseline

RouteGate uses role-based access control for Manager API access.

This baseline keeps the first admin session boundary explicit:

- authenticated users have roles and permissions;
- admin sessions require an active human user;
- admin sessions require one of the admin roles;
- portal users are not admin users;
- Agent credentials are not admin sessions;
- legacy `/api/admin/*` routes must not bypass the admin session boundary.

## Admin roles

Current admin roles:

- `super_admin`
- `admin`
- `operator`
- `read_only`

## Portal and Agent roles

Current non-admin roles:

- `vpn_user`
- `agent`

These roles are intentionally not accepted by `RequireAdminSession`.

## Session audit events

Current session lifecycle audit events:

- `auth.login.success`
- `auth.login.failure`
- `auth.logout.success`
- `auth.logout.failure`

Audit metadata must not contain raw session tokens or password values.

## Future work

Future hardening can add:

- session listing and revocation UI;
- per-admin session inventory;
- role assignment audit events;
- password change audit events;
- optional MFA;
- stricter permission mapping for every legacy route.
