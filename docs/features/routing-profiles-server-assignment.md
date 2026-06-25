# RG-62 — Routing Profiles Server Assignment

RG-62 adds the first server-level assignment workflow for RouteGate routing profiles.

## Goal

Admins can assign one routing profile to a server, replace the assignment, or clear it.

The selected profile is used by the existing RG-60 server and client config renderers through the `server_routing_profiles` table.

## Admin API

New authenticated endpoints are available under `/api/v1/servers/{server_id}/routing-profile`:

- `GET /api/v1/servers/{server_id}/routing-profile` — read the explicit server assignment.
- `PUT /api/v1/servers/{server_id}/routing-profile` — assign or replace the server routing profile.
- `DELETE /api/v1/servers/{server_id}/routing-profile` — clear the explicit assignment.

The assignment request body is:

```json
{
  "routingProfileId": "profile-uuid"
}
```

The response includes the server id, assigned profile metadata, and assignment timestamps when an explicit assignment exists.

## Admin UI

The server details page now includes a `Routing profile` section.

Admins can:

- see the current explicit routing profile assignment;
- select a profile from existing routing profiles;
- save or replace the assignment;
- clear the assignment.

## Render behavior

When a server has an explicit routing profile assignment, the existing rendered config flow uses that profile's enabled rules.

When a server has no explicit assignment, the existing safe/default behavior is kept: the render path falls back to the default routing profile if one exists.

## Out of scope

RG-62 intentionally does not add:

- account-level routing profile assignment;
- user/group/team profile inheritance;
- per-rule UI changes;
- preset rule packs;
- traffic stats changes;
- OPNsense integration.
