# RG-61 — Routing Profiles Admin UI

RG-61 adds the first administrative surface for RouteGate routing profiles and split-tunnel rules.

## Admin API

New authenticated endpoints are available under `/api/v1/routing-profiles`:

- `GET /api/v1/routing-profiles` — list routing profiles.
- `POST /api/v1/routing-profiles` — create a routing profile.
- `GET /api/v1/routing-profiles/{profile_id}` — get profile details with rules.
- `PATCH /api/v1/routing-profiles/{profile_id}` — update profile metadata/default flag.
- `DELETE /api/v1/routing-profiles/{profile_id}` — delete a non-default profile.
- `POST /api/v1/routing-profiles/{profile_id}/rules` — create a routing rule.
- `PATCH /api/v1/routing-profiles/{profile_id}/rules/{rule_id}` — update a routing rule.
- `DELETE /api/v1/routing-profiles/{profile_id}/rules/{rule_id}` — delete a routing rule.

The endpoints use the existing `routing_profiles:*` permissions.

## Rule actions

Supported actions remain the RG-60 foundation actions:

- `direct`
- `vpn`
- `block`

The UI stores rule matchers as arrays and lets admins enter values line-by-line or comma-separated.

## Admin UI

The Admin UI now includes a `Routing Profiles` navigation item. The page supports:

- creating routing profiles;
- viewing existing profiles;
- editing selected profile metadata;
- creating routing rules;
- editing routing rules;
- deleting routing rules;
- deleting non-default profiles.

Default profiles cannot be deleted. Disabled rules remain saved but are ignored by rendering because RG-60 renderers only load enabled rules.

## Out of scope

RG-61 intentionally does not add:

- server-to-profile assignment UI;
- predefined rule packs;
- GeoIP/Geosite import;
- per-user routing profiles;
- Clash/V2Ray renderers;
- OPNsense integration.
