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

The persisted `vpn_client_profiles.client_type` value is the primary source of client identity.

Initial explicit values are:

- `hiddify`
- `v2rayn`
- `v2box`
- `v2raytun`
- `sing-box`
- `other`

No database migration is required because the field is already textual.

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

When `format` is omitted or `auto`, RG-115A selects a representation from the resolved client profile:

- Hiddify / sing-box on VLESS: full sing-box JSON rendered by the existing `RenderSingBoxClientConfig` path;
- v2rayN: standard Base64 share-link subscription;
- V2Box: standard Base64 share-link subscription;
- V2RayTun: standard Base64 share-link subscription plus its supported subscription `routing` header;
- unknown clients: conservative RG-115 `auto` behavior.

An explicit `?format=` request still overrides automatic representation selection for compatibility and diagnostics.

## Hiddify / sing-box adapter

The adapter reuses `RenderSingBoxClientConfig`.

The existing RouteGate renderer already translates Routing Profile actions to sing-box outbounds:

- `DIRECT` -> `direct`
- `VPN` -> `routegate-out`
- `BLOCK` -> `block`

The adapter therefore contains no routing-policy decisions of its own.

## V2RayTun adapter

V2RayTun supports a subscription `routing` header containing Base64-encoded routing JSON. Its documentation states that subscription routing takes precedence over routing configured in the application and Direct Service.

RouteGate mechanically translates the already-resolved Routing Profile:

- RouteGate `DIRECT` -> V2RayTun `direct`
- RouteGate `VPN` -> V2RayTun `proxy`
- RouteGate `BLOCK` -> V2RayTun `block`
- exact domains -> `full:`
- suffixes -> `domain:`
- keywords -> `keyword:`
- GeoSite -> `geosite:`
- CIDRs remain CIDRs
- GeoIP -> `geoip:`

This adapter does not decide which traffic should be direct/proxied/blocked; it only serializes the existing decision.

V2RayTun remains `partial_compatibility` because TUN and DNS runtime settings remain client-side and may affect deterministic DNS/split-DNS behavior.

## v2rayN and V2Box

Both clients expose strong local routing and DNS functionality, but RouteGate currently delivers standard share-link subscriptions to them. That representation carries connection material, not the complete RouteGate Routing Profile.

They are therefore classified as `client_setup_required`. Admin UI guidance must make this visible rather than silently degrading to ordinary VPN connectivity.

## Subscription metadata

Known clients receive compatible subscription metadata where supported:

- `Profile-Title` uses the `base64:` prefix when Base64 encoded;
- Hiddify and V2RayTun receive a `Profile-Update-Interval` hint;
- V2RayTun receives `Routing` when a RouteGate Routing Profile is present;
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
