# RG-116 Access & Devices (Per-Device Access Model)

## Status

Initial implementation.

## Decision

A VPN Account is a user/account identity. A single VPN Account may have
multiple devices / access instances, each with its own opaque RG-115
subscription token, client type, and independent revoke/rotate lifecycle:

```text
VPN Account: Felix
  +-- iPhone       (client: Hiddify,  token A)
  +-- Windows PC   (client: v2rayN,   token B)
  +-- Android      (client: v2rayNG,  token C)
```

A device is an **access/delivery credential**, not a second VPN identity.
The account's underlying protocol credentials (VLESS UUID, Reality/WireGuard
keys, enabled protocol set) remain a single account-level identity
(`vpn_client_profiles`, `vpn_account_protocols`) so that revoking one
device's link never touches another device's connectivity, and rotating a
protocol credential is never required just to add a device.

## Schema

Migration `000149_vpn_account_devices` adds:

- `vpn_account_devices(id, vpn_account_id, name, client_type, device_type,
  status, created_at, updated_at, last_used_at, revoked_at)` — one row per
  device.
- `vpn_subscription_tokens.device_id` (nullable FK) — a device's opaque RG-115
  token. The "one active token" uniqueness constraint moves from
  `vpn_account_id` to `device_id`: a compromised device's token can be
  rotated or revoked without invalidating any other device on the same
  account.

Existing installations are migrated forward, not broken: every account that
already had an active subscription token gets one backfilled "Default
device" (carrying over its prior account-level `client_type`/`device_type`),
and that existing token is attached to it — the existing `/sub/<token>` URL
keeps resolving exactly as before. Accounts without an active token are left
alone; they get devices organically through "Add device".

The pre-existing account-level subscription-token endpoints
(`POST/DELETE /vpn-accounts/{id}/subscription-token[/rotate]`) are untouched
for backward compatibility. New devices are managed exclusively through the
new `/vpn-accounts/{id}/devices` endpoints.

Backfill normalizes the legacy `vpn_client_profiles.client_type`/`device_type`
vocabulary onto the RG-116 device allow-list rather than copying it verbatim:
`hiddify`/`v2rayn`/`v2rayng` (and `windows`/`ios`/`android`/`macos`/`linux`)
pass through unchanged, and everything else - `v2raytun`, `v2box`, `other`,
`sing-box`, or any older/unknown value - normalizes to `generic`/`other`,
matching `normalizeClientType` in `client_capabilities.go`. A backfilled
device is never created with a `client_type` the device API's own validation
(`allowedDeviceClientTypes`) would then reject on the next rename/rotate.

## Token isolation: legacy, device, and Portal tokens never cross

RG-116 intentionally allows two token families to coexist on one account: at
most one legacy token (`device_id IS NULL`, from the pre-existing
account-level endpoints and the Portal's self-service subscription) and at
most one active token per device. Every repository method that creates or
revokes a *legacy* token — `vpnaccounts.Repository.CreateSubscriptionToken`,
`RevokeActiveSubscriptionTokens`, `GetActiveSubscriptionTokenByHash`, and
`portal.Repository.CreateSubscriptionToken` — filters its `UPDATE`/`SELECT`
on `device_id IS NULL`. None of them may ever revoke or match a device's own
token; device tokens are created/rotated/revoked exclusively through
`device.go`'s device-scoped methods, which filter on `device_id` instead.
This is enforced at the application layer (these queries) and backstopped by
the two independent partial unique indexes from the Schema section above.

The Portal remains, deliberately, the legacy account-level access path: it
does not yet know about individual devices, and this pass does not give it
one. Its self-service subscription token is exactly the same
`device_id IS NULL` legacy token the account-level HTTP endpoints issue, so
it is subject to the same isolation guarantee - generating or rotating a
Portal link never touches any RG-116 device. If Portal self-service ever
needs its own per-device identity, that is a future, separate product
decision (a "Portal → Access & Devices" migration), not something this pass
introduces silently.

## Device access URL uses the canonical PublicURL, not the request Host

A device's issued `/sub/<token>` link must always pass RouteGate's own Send
validator (`delivery.extractCanonicalSubscriptionToken`), which checks the
URL's scheme/host against the administrator's configured RouteGate
PublicURL exactly. So device token issuance and rotation
(`Handler.deviceSubscriptionURL`) build that URL from the same configured
PublicURL (via the shared `internal/publicurl` policy also used by
`delivery.NormalizePublicURL`) instead of the incoming request's
`Host`/`X-Forwarded-Host`/`X-Forwarded-Proto`. Forwarded headers can
therefore never move where a device's credential resolves. If PublicURL is
not configured yet, device issuance falls back to the same request-derived
origin the legacy endpoint has always used (Send does not work without a
valid PublicURL either way, so this fallback can never produce a link Send
would otherwise have accepted from a different origin).

The legacy account-level endpoint (`Handler.subscriptionURL`) intentionally
keeps its original request-derived behavior for backward compatibility
(pinned by `TestCreateSubscriptionTokenFallsBackFromInvalidForwardedHeaders`);
Send validation for that endpoint's own share/QR flow does not apply the
same strict canonical-origin check RG-116 device Send does, so this is a
deliberate, documented divergence rather than an oversight. The Portal's
subscription URL construction is unchanged for the same reason.

## Access & Devices owns Send

The device card is the only place an administrator sends access; there is no
separate account-level "Send" concept in the UI. This reuses the existing
delivery stack end to end - providers, the durable queue/worker, retry,
history, Telegram pairing, templates - it does not duplicate any of it.

The one real constraint is RG-115 itself: a subscription token is stored
hash-only and its plaintext is returned exactly once, on issue/rotate. A
device's Send button is therefore enabled only while the frontend still
holds that plaintext URL in memory (immediately after Create or Rotate) and
passes it explicitly in the send request. The backend:

1. confirms the device belongs to the account and is active;
2. confirms the caller-supplied URL is genuinely RouteGate's own canonical
   `/sub/<token>` link - not merely *some* URL that happens to embed a valid
   token (`delivery.extractCanonicalSubscriptionToken`): scheme and host
   must match the configured RouteGate public URL exactly, the path must be
   exactly `/sub/<token>` (no extra segments, no query string, no
   fragment), and the URL must not carry embedded userinfo. A same-token
   link on a foreign host (`https://evil.example/sub/<valid-token>`) is
   rejected before the token is even compared, so this can't be used as an
   open relay for an attacker-chosen link;
3. re-hashes the extracted token and confirms it matches the device's own
   current active token hash (`delivery.validateDeviceAccessRequest`);
4. stashes the plaintext in `deviceAccessMaterialStore`, an in-memory,
   process-local map keyed by the new delivery row's ID
   (`backend/internal/delivery/device_access_material.go`) - never written
   to Postgres, a log, or a delivery history record;
5. enqueues a normal `Delivery` row (now carrying a `device_id`, metadata
   only) that the existing worker picks up and resolves through
   `VPNAccessResolver`, which serves device-scoped deliveries from that
   store instead of re-deriving material from account credentials.

The stashed entry has a ~30-minute TTL as a backstop, but the worker also
releases it explicitly the moment a delivery reaches a terminal outcome for
its current attempt - sent, delivered, permanently failed, or uncertain -
rather than leaving the plaintext sitting in memory for the rest of the TTL.
A delivery that is merely scheduled to retry keeps its material, since the
worker will resolve it again on the next attempt.

If the process restarts, the material is released early, or the TTL elapses
before the worker sends it, resolution fails permanently with
`device_access_link_unavailable` rather than fabricating or recovering a
URL - the admin re-sends from the still-revealed link, or rotates first.
This is a deliberate trade-off: it keeps device-scoped Send fully within the
"never persist a bearer token" rule at the cost of not supporting delivery
*after* the reveal window has passed for that exact link (rotating always
produces a fresh, sendable one).

Delivery history keeps working unchanged; each device's card shows only the
history rows tagged with its own `device_id`, so one device's send activity
is never visible on another device's card.

## Client support strategy

RouteGate supports protocols broadly, but supports VPN clients selectively:

1. **Hiddify** — primary/recommended client. Full RouteGate experience
   (sing-box config + Routing Profile) where the effective protocol is VLESS.
2. **v2rayN** — officially supported desktop client (2dust family).
   Connection/subscription support, plus an implemented, automated-test-covered
   native routing-rules import (`format=v2rayn-routing`); routing requires
   local client setup, and manual real-client runtime validation (historically
   tracked in issue #392, which is closed - the validation itself is what
   remains pending) is still outstanding, so this is not yet claimed as
   independently confirmed.
3. **v2rayNG** — officially selectable Android client (2dust family) for
   standard connection/subscription delivery. **Not** promoted to the same
   routing tier as v2rayN merely because it shares a client family: its
   native routing-rules import has not been independently validated on a
   real client, so it stays `connection_only` (see
   `client-compatibility-matrix.md`) until that validation happens.
4. **Generic** — standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto
   connection material. Best-effort connectivity only; no RouteGate
   routing/DNS policy guarantee.

V2RayTun, V2Box, Streisand, FoXray, Amnezia, and similar clients are not
first-class RouteGate clients and use Generic compatibility. See
"RG-115B retirement" below.

Compatibility is exposed to the Admin UI in the existing RG-115A three-state
model (`full_smart_routing`, `client_setup_required`, `connection_only`),
simplified in the Access & Devices UI to **Full RouteGate / Compatible /
Generic**.

## RG-115B retirement

RG-115B introduced native routing adapters for v2rayN, V2RayTun, and V2Box,
plus a V2RayTun HTTPS-redirect handoff (issue #398, PR #404). Manual
validation showed the V2RayTun/V2Box-specific pieces were brittle and kept
growing bespoke UI/adapters for clients RouteGate does not officially
support.

Kept (still valuable, officially supported):

- the v2rayN native custom-routing-rules adapter and its
  `format=v2rayn-routing` delivery format;
- the mechanical rule-mapping helpers (`routingRuleOutboundTag`,
  `routingRuleDomainConditions`, `routingRuleIPConditions` in
  `v2rayn_adapter.go`) — these contain no client-specific policy, only a
  DIRECT/VPN/BLOCK → client vocabulary mapping, and are the shared
  foundation any future adapter would reuse.

Retired (dead ends under the new client strategy):

- `v2raytun_adapter.go`, `v2box_adapter.go` and their
  `format=v2raytun-routing` / `format=v2box-routing` delivery formats and the
  V2RayTun subscription `Routing` header / `handoff=1` redirect;
- the frontend `ClientRoutingImport.tsx` V2RayTun/V2Box QR/deep-link UI and
  the `ShareAccessActions` raw-material share panel it depended on;
- the `ClientTypeV2RayTun` / `ClientTypeV2Box` compatibility branches — these
  values still normalize (for old persisted rows) but map to Generic instead
  of a bespoke tier.

Issue #398 and PR #404 are superseded by this document; see the PR
description for the exact disposition.

## Non-goals

This does not change:

- the RG-115 opaque `/sub/<token>` URL shape or its security properties
  (hash-only storage, no-store/no-referrer headers, no infrastructure
  details in the URL);
- the account-level VPN identity/credential model;
- Routing Profiles as the single routing-policy source (RG-115A's layering
  is unchanged; a device only selects which representation of the already
  resolved policy it receives).
