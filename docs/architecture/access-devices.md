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

## Client support strategy

RouteGate supports protocols broadly, but supports VPN clients selectively:

1. **Hiddify** — primary/recommended client. Full RouteGate experience
   (sing-box config + Routing Profile) where the effective protocol is VLESS.
2. **v2rayN** — officially supported desktop client (2dust family).
   Connection/subscription support; routing may require local client setup.
3. **v2rayNG** — officially supported Android client (2dust family). Same
   compatibility posture as v2rayN.
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
