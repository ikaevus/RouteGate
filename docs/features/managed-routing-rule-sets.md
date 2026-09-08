# Managed routing rule sets

RG-EXP-01 extends Routing Profiles with centrally managed sing-box source rule
sets and route diagnostics. It does not introduce a second routing subsystem or
change Node Group selection.

## Effective policy

Every profile has a `defaultAction` (`vpn` for migrated profiles). Enabled
manual rules and successfully refreshed managed rule sets share one ordered
policy:

1. lower numeric priority wins;
2. a manual administrator rule wins a priority tie with a managed set;
3. stable IDs break remaining ties.

This permits a Russia-oriented profile with `direct` as its default, blocked
resources routed to `vpn`, optional local-only managed resources routed to
`direct`, and manual `direct`, `vpn`, or `block` overrides.

## Refresh safety

Managed sources use sing-box source rule-set JSON (`version: 1`). Manager limits
downloads to 32 MiB, rejects credential-bearing or private-network URLs, and
validates a non-empty document before committing it. Failed refreshes update
health information but retain the last successful snapshot.

Client configurations reference RouteGate's read-only snapshot endpoint rather
than the upstream URL. This keeps last-known-good behavior for both existing and
new clients when an upstream source later fails. The generated sing-box config
uses remote `rule_set` entries and downloads them through the client's native
`direct` outbound.

## Admin API

- `GET /api/v1/managed-routing-rule-sets?profileId=...`
- `POST /api/v1/routing-profiles/{profile_id}/managed-rule-sets`
- `PATCH|DELETE /api/v1/managed-routing-rule-sets/{set_id}`
- `POST /api/v1/managed-routing-rule-sets/{set_id}/refresh`
- `POST /api/v1/routing-profiles/{profile_id}/diagnostics`
- `GET /api/public/routing-rule-sets/{set_id}` (validated snapshot only)

The management endpoints use existing `routing_profiles:*` permissions and are
not exposed in the User Portal.
