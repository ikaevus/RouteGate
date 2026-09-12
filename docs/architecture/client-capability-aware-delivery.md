# RG-115A Client Capability-Aware Config Delivery

## Status

Initial implementation.

## Decision

RouteGate treats secure delivery, routing policy, and client compatibility as separate layers:

```text
Routing Profiles
  WHAT traffic policy should apply
        |
        v
Client capability / adapter layer (RG-115A)
  WHICH representation can faithfully carry that policy
        |
        v
Secure subscription delivery (RG-115)
  HOW the credential and representation are delivered
        |
        v
/sub/<opaque-token>
```

Protocol compatibility does not imply routing-policy compatibility.

The client capability layer must never create a second routing-policy engine. It consumes the already-resolved `SubscriptionProfile.RoutingProfile` and uses existing RouteGate renderers or mechanical client-format adapters.

## Client identity

The persisted `client_type` value is the primary source of client identity — on `vpn_account_devices.client_type` per device (RG-116), or on the legacy account-level `vpn_client_profiles.client_type` for the underlying protocol-rendering profile.

Officially supported values (RouteGate supports protocols broadly, but clients selectively; see [RG-116](access-devices.md)):

- `hiddify` (recommended)
- `v2rayn`
- `v2rayng`
- `sing-box`
- `generic`

Legacy values `v2raytun`, `v2box`, and `other` remain part of this field's accepted vocabulary - both on existing persisted rows and for new writes to this account-level endpoint, which predates RG-116 and was never narrowed by it - but always normalize to `generic` for capability purposes, since RouteGate does not maintain bespoke adapters for them. RG-116's own, separate Access & Devices allow-list (`hiddify`, `v2rayn`, `v2rayng`, `generic`) is narrower and is what new device rows and their migration backfill are validated against; see `access-devices.md`. No database migration was required for this account-level vocabulary because the field is already textual.

Automatic detection is deliberately conservative. If the saved profile is `other`, RouteGate may recognize an unambiguous client `User-Agent`. Explicit administrator selection always wins over automatic detection.

## Capability model

The computed capability model records whether a client can support:

- full sing-box configuration import;
- URI/subscription import;
- TUN mode;
- DIRECT / VPN / BLOCK routing;
- remote rule sets;
- DNS routing and split DNS;
- client-local routing rules;
- subscription refresh;
- subscription-delivered routing policy;
- known imported-rule precedence.

Capabilities are returned with the existing client-connection response as a computed `clientCompatibility` object. They are not persisted as another source of truth.

## Compatibility states

### `full_smart_routing`

RouteGate can deliver a representation that carries the RouteGate routing policy for the supported scenario without requiring the administrator to recreate those rules locally.

### `client_setup_required`

The client can provide the required networking features, but the standard subscription representation cannot by itself enforce the RouteGate Routing Profile. RouteGate therefore supplies explicit client-side guidance and the Admin UI must not claim that Smart Routing is fully enforced.

### `partial_compatibility`

RouteGate can automatically deliver part of the policy (for example routing rules), while another material part of behavior (for example TUN/DNS runtime behavior) remains controlled by the client.

### `connection_only`

Only protocol-level connectivity is assumed. RouteGate does not claim routing-policy compatibility.

## Delivery selection

`GET /sub/{token}` remains the sole user-facing RG-115 credential boundary.

When `format` is omitted or `auto`, RG-115A selects a representation from the resolved client identity (device client type, falling back to the account-level profile and then User-Agent detection):

- Hiddify / sing-box on VLESS: full sing-box JSON rendered by the existing `RenderSingBoxClientConfig` path;
- v2rayN / v2rayNG: standard Base64 share-link subscription;
- Generic / unknown clients: conservative RG-115 `auto` behavior.

An explicit `?format=` request still overrides automatic representation selection for compatibility and diagnostics.

## Hiddify / sing-box adapter

The adapter reuses `RenderSingBoxClientConfig`.

The existing RouteGate renderer already translates Routing Profile actions to sing-box outbounds:

- `DIRECT` -> `direct`
- `VPN` -> `routegate-out`
- `BLOCK` -> `block`

The adapter therefore contains no routing-policy decisions of its own.

## v2rayN

v2rayN exposes strong local routing and DNS functionality, but RouteGate currently delivers standard share-link subscriptions to it by default. That representation carries connection material, not the complete RouteGate Routing Profile; the separate `v2rayn-routing` native adapter (see `docs/architecture/client-compatibility-matrix.md`) carries the policy as an additional import. The adapter's mechanical rule mapping is implemented and covered by automated tests, but real-client runtime validation (does the imported profile actually make Ozon/Wildberries DIRECT on a real v2rayN install) is still pending — manual runtime validation remains pending, historically tracked in issue #392 (which is closed; only the validation itself is outstanding).

v2rayN is therefore classified as `client_setup_required` on the strength of the implemented, tested adapter, not on a claim that its real-client routing behavior has already been confirmed. Admin UI guidance must make this visible rather than silently degrading to ordinary VPN connectivity.

## v2rayNG

v2rayNG (the Android member of the same 2dust client family) also receives standard share-link subscriptions by default, but RouteGate does **not** offer it the `v2rayn-routing` native adapter and does not claim `client_setup_required` for it: that would assume v2rayNG's native routing-rules import behaves like v2rayN's without having independently validated it on a real client. Per RG-116's "no silent downgrade" rule, family membership is not evidence of compatibility.

v2rayNG stays `connection_only` until a real client validates the same routing behavior, at which point this doc, the client capability model, and the compatibility matrix should be updated together.

## Generic clients (V2RayTun, V2Box, and others)

RouteGate does not maintain a bespoke adapter, routing header, or deep-link flow for these clients (retired RG-115B work; see [RG-116](access-devices.md#rg-115b-retirement)). They receive standard connection material and are classified `connection_only`.

## Subscription metadata

Known clients receive compatible subscription metadata where supported:

- `Profile-Title` uses the `base64:` prefix when Base64 encoded;
- Hiddify receives a `Profile-Update-Interval` hint;
- RouteGate diagnostic headers expose resolved client type, compatibility state, and selected delivery representation.

The RG-115 no-store/no-referrer and token-log protections remain unchanged.

## No silent downgrade rule

When an effective Routing Profile exists, RouteGate distinguishes:

1. protocol-level ability to render client routing;
2. selected-client ability to reproduce that policy.

The Admin UI shows the client compatibility state and limitations. A non-full client is never presented as fully enforcing the RouteGate Routing Profile merely because the protocol connection itself works.

## Extension rules

Adding a new client must:

1. add or update one capability profile;
2. select or implement a delivery adapter;
3. reuse resolved Routing Profile data rather than reimplementing policy logic;
4. document imported-rule precedence and DNS/TUN assumptions;
5. add deterministic tests and a compatibility-matrix entry.
