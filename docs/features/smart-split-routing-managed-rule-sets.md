# Smart split tunneling and managed rule sets (RG-EXP-01)

Experiment baseline: `ccec8aca319c809a6317b7eb85b26e72834aaa09`.
Branch: `exp/rg-smart-routing-astra`. This experiment does not change `main`.

## Using a Russia smart routing profile

1. Open **Routing Profiles**, select/create a profile, and set its default action
   to **DIRECT**. Existing profiles retain **VPN** as their default action.
2. In **Managed Rule Sets**, select **Re:filter** or **RunetFreedom**, keep action
   **VPN**, and save. Sources created by the UI start disabled.
3. Refresh the source. Check the successful update timestamp, then enable it.
4. Optionally add an administrator-owned sing-box source JSON for RU-only resources
   with action **DIRECT**. There is no blanket `*.ru` rule or built-in RU-only pack.
5. Add manual DIRECT/VPN/BLOCK exceptions with a lower numerical priority than the
   managed rules they override. The UI defaults are manual 1000, managed 2000.
6. Assign the profile to a server or VPN account. Account overrides, server
   assignments, and global fallback keep their existing precedence.
7. Refresh the client's **sing-box JSON subscription**. The existing mixed inbound
   can be used as a local proxy; this stage does not add a TUN interface. Native URI
   formats such as `vless://` do not carry these routing rules.

DIRECT uses the client's direct outbound. VPN uses the account's current VLESS
outbound and does not choose another Node Group or VPN exit. BLOCK uses sing-box's
`reject` route action. No managed rule is applied as post-VPN server routing.

## Storage, updates and safety

Migration `000146_managed_routing_rule_sets` adds:

- `routing_profiles.default_action`, defaulting to `vpn`;
- `routing_profile_managed_sets`, associated with the existing profile, containing
  source identity, action, enabled state, priority, update interval, a validated
  JSONB snapshot, SHA-256, last attempt/success timestamps, and last error.

Provider metadata and source decoders are independent from the refresh lifecycle.
The initial presets consume the providers' actual text sources:

- [Re:filter domain list](https://github.com/1andrevich/Re-filter-lists)
  (`main/domains_all.lst`);
- [RunetFreedom](https://github.com/runetfreedom/russia-blocked-geosite)
  (`release/ru-blocked.txt`).

Those text sources are converted to modern headless rules, not GeoSite database
references. `domain`, `full`, `keyword`, and `regexp` text directives are supported;
unknown directives, including unresolved `include`, fail validation. Custom HTTPS
URLs accept sing-box source JSON versions 1–3 with `domain`, `domain_suffix`,
`domain_keyword`, `domain_regex`, and `ip_cidr`. Other headless fields, logical
rules, and binary `.srs` input are deliberately rejected. A single DNS label is
allowed in source matchers, and a trailing dot is normalized in text lists.

A Manager worker checks due enabled sources on startup and once per minute.
Refresh intervals are 1–168 hours, default 24 in the UI; failures retry at the same
interval. Manual refresh also works on disabled sources. A database row lock
serializes refresh with updates/deletion, including across Manager instances.
Downloads and parsed snapshots are bounded to 16 MiB, 100000 headless rules and
500000 matchers. Each profile supports at most 16 sources.

Updates validate before the atomic replacement. Errors or empty documents retain
all previous snapshot data and last-success metadata. The failure status is
persisted. An enabled source without any successful snapshot prevents client
configuration delivery and produces an explicit diagnostic error. Previously
issued client configurations continue to use their embedded snapshot.

URLs require HTTPS on port 443 without credentials/fragments. DNS results are
checked and the actual connection address is pinned; private, loopback, link-local
and reserved ranges are denied, including redirects. Downloads have bounded
redirects and timeouts, and do not use proxy environment variables. A Manager
therefore needs direct outbound HTTPS access to its configured sources.

A source's provider and URL are immutable. To replace a source, create and refresh
its replacement before switching enabled states. This avoids labeling a cached
snapshot as data from a different source after a failed URL change.

## One policy for production and diagnostics

`routingprofiles.CompilePolicy` combines enabled manual and managed entries using:

1. ascending numerical priority;
2. ascending creation timestamp;
3. ascending UUID as a deterministic tie-breaker.

Both production subscriptions and diagnostics consume this same representation.
The client renderer emits snapshot-backed inline `route.rule_set` entries and
ordered references in `route.rules`. Matching destination conditions use sing-box's
OR grouping. Manual rules retain their existing match fields; legacy GeoSite/GeoIP
fields are not modernized by this change and still depend on client compatibility.

Diagnostics accepts a domain (ASCII/punycode) or IP and an optional client-resolved
IP for domain tests. It reports the profile, action, winning rule kind, priority,
order, creation time, ID, and managed snapshot hash. It tests the selected profile;
account/server effective-profile selection remains visible in the existing account
assignment UI and is reused by production delivery.

The Manager does not resolve DNS on behalf of the client. If an earlier IP rule
could match an unknown resolved address, diagnostics returns `indeterminate`, not
a guessed default. Legacy GeoSite/GeoIP rules similarly require unavailable client
database data. A diagnostic describes the current Manager snapshot; clients must
refresh their subscriptions to receive it.

The new renderer is checked with **sing-box 1.12.0**. Inline rule sets require
sing-box 1.10+, and route `reject` actions require 1.11+. This stage targets modern
sing-box JSON clients; it does not translate routing into native WireGuard,
Hysteria2, Shadowsocks, MTProto, Clash or raw VLESS URIs.

## Administrative API

All endpoints retain existing `routing_profiles:read` / `routing_profiles:update`
permission checks. They are not exposed as User Portal routing-editing APIs.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/routing-rule-set-providers` | Presets and formats |
| GET | `/api/v1/routing-profiles/{profile_id}` | Includes source health without snapshot contents |
| POST | `/api/v1/routing-profiles/{profile_id}/managed-sets` | Create source |
| PUT | `/api/v1/routing-profiles/{profile_id}/managed-sets/{set_id}` | Update name, action, priority, enabled state, interval |
| DELETE | `/api/v1/routing-profiles/{profile_id}/managed-sets/{set_id}` | Remove source |
| POST | `/api/v1/routing-profiles/{profile_id}/managed-sets/{set_id}/refresh` | Attempt refresh, return persisted health |
| POST | `/api/v1/routing-profiles/{profile_id}/diagnostics` | Test `{ "destination": "example.org", "resolvedIp": "" }` |

Profile create/update accepts `defaultAction`. Managed create/update body:

```json
{
  "name": "RU Blocked",
  "provider": "runetfreedom",
  "sourceUrl": "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/ru-blocked.txt",
  "priority": 2000,
  "action": "vpn",
  "enabled": false,
  "refreshHours": 24
}
```

A refresh returning HTTP 200 means the attempt was stored: check `lastError` and
`lastSuccessAt` to determine whether it succeeded. Concurrent refresh can return
404 because its row is locked. Provider/URL mismatch on PUT also returns 404.

## Validation and operational limits

- Backend unit suites and builds; frontend build, i18n and `npm run test:routing`.
- Policy tests cover actions, managed matches, manual overrides, ordering, disabled
  sources, missing snapshots, domain boundaries, CIDRs, regex and uncertain results.
- `TestManagedRoutingMigrationInheritanceRefreshAndDelivery` runs against an
  explicitly supplied `ROUTEGATE_TEST_DATABASE_URL`. It resets that test database's
  public schema, applies migrations, tests profile inheritance, successful/failed/
  empty refresh, last-known-good delivery, manual override, disable/delete, default
  updates and migration rollback. Never point it at a live database.
- Optional `ROUTEGATE_TEST_SING_BOX=/path/to/sing-box` validates actual generated
  client configs for all three default actions.
- Optional `ROUTEGATE_TEST_REFILTER_SOURCE` and
  `ROUTEGATE_TEST_RUNETFREEDOM_SOURCE` validate downloaded real provider fixtures
  without making ordinary unit tests depend on external availability.

During this benchmark, migration/lifecycle integration was validated using
PostgreSQL WASM (PGlite), not a native PostgreSQL service. This does not establish
multi-process lock contention behavior. Actual sing-box configuration checks and
both real provider fixture validations passed. Browser visual acceptance could
not be completed because the browser-side local preview listener was denied by
the environment. Frontend compilation, localization and state-model tests are
separate checks, not a replacement for visual acceptance.

No multi-exit routing, Node Group rule selectors, portal rule editing, or unrelated
UI redesign is included. No dependency was added to the application runtime.
